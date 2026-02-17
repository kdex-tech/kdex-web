package host

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/kdex-tech/kdex-host/internal/auth"
	"github.com/kdex-tech/kdex-host/internal/host/ico"
	ko "github.com/kdex-tech/kdex-host/internal/openapi"
	"github.com/kdex-tech/kdex-host/internal/page"
	"github.com/kdex-tech/kdex-host/internal/sniffer"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kdexUIMetaTemplate = `<meta
	name="kdex-ui"
	data-navigation-endpoint="/-/navigation/{name}/{l10n}/{basePathMinusLeadingSlash...}"
	data-openapi-endpoint="/-/openapi"
	data-page-basepath="%s"
	data-page-patternpath="%s"
	data-path-separator="/-/"
	/>
	`
)

type HostHandler struct {
	Mux          *http.ServeMux
	Name         string
	Namespace    string
	Pages        *page.PageStore
	Translations Translations

	analysisCache             *AnalysisCache
	authConfig                *auth.Config
	client                    client.Client
	defaultLanguage           string
	favicon                   *ico.Ico
	functions                 []kdexv1alpha1.KDexFunction
	host                      *kdexv1alpha1.KDexHostSpec
	importmap                 string
	log                       logr.Logger
	mu                        sync.RWMutex
	openapiBuilder            ko.Builder
	packageReferences         []kdexv1alpha1.PackageReference
	pathsCollectedInReconcile map[string]ko.PathInfo
	registeredPaths           map[string]ko.PathInfo
	scripts                   []kdexv1alpha1.ScriptDef
	themeAssets               []kdexv1alpha1.Asset
	translationResources      map[string]kdexv1alpha1.KDexTranslationSpec
	utilityPages              map[kdexv1alpha1.KDexUtilityPageType]page.PageHandler

	authChecker interface {
		CalculateRequirements(resource string, resourceName string, kdexreqs []kdexv1alpha1.SecurityRequirement) ([]kdexv1alpha1.SecurityRequirement, error)
		CheckAccess(ctx context.Context, resource string, resourceName string, requirements []kdexv1alpha1.SecurityRequirement) (bool, error)
	}

	authExchanger interface {
		AuthCodeURL(state string) string
		EndSessionURL() (string, error)
		ExchangeCode(ctx context.Context, code string) (string, error)
		ExchangeToken(ctx context.Context, issuer string, rawIDToken string) (string, error)
		GetClientID() string
		GetTokenTTL() time.Duration
		LoginLocal(ctx context.Context, issuer string, username, password string, scope string) (string, string, error)
	}

	sniffer interface {
		Analyze(r *http.Request) (*sniffer.AnalysisResult, error)
		DocsHandler(w http.ResponseWriter, r *http.Request)
	}
}

type Translations struct {
	catalog *catalog.Builder
	keys    []string
}

func (t *Translations) Catalog() *catalog.Builder {
	return t.catalog
}

func (t *Translations) Keys() []string {
	return t.keys
}

func (t *Translations) Languages() []language.Tag {
	return t.catalog.Languages()
}

type errorResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	statusMsg   string
	wroteHeader bool
}

type wrappedWriter struct {
	*errorResponseWriter
	f http.Flusher
	h http.Hijacker
	p http.Pusher
}

func (w *wrappedWriter) Flush() {
	w.errorResponseWriter.Flush()
}

func (w *wrappedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.errorResponseWriter.Hijack()
}

func (w *wrappedWriter) Push(target string, opts *http.PushOptions) error {
	return w.errorResponseWriter.Push(target, opts)
}

func GetErrorResponseWriter(w http.ResponseWriter) *errorResponseWriter {
	if ew, ok := w.(*errorResponseWriter); ok {
		return ew
	}
	if wrapped, ok := w.(*wrappedWriter); ok {
		return wrapped.errorResponseWriter
	}
	return nil
}

func wrappedErrorResponseWriter(ew *errorResponseWriter, w http.ResponseWriter) http.ResponseWriter {
	// Check what the original writer supports
	f, _ := w.(http.Flusher)
	h, _ := w.(http.Hijacker)
	p, _ := w.(http.Pusher)

	return &wrappedWriter{ew, f, h, p}
}

var _ http.ResponseWriter = (*errorResponseWriter)(nil)

// Flush implements the http.Flusher interface.
func (ew *errorResponseWriter) Flush() {
	// If we have an error code, we don't want to flush the
	// "bad" body yet, as the middleware will handle it.
	if ew.statusCode >= 400 {
		return
	}

	// Pass the flush signal to the underlying ResponseWriter
	if flusher, ok := ew.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack lets the caller take over the connection (needed for WebSockets)
func (ew *errorResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := ew.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support Hijacker")
}

// Push implements HTTP/2 server push
func (ew *errorResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := ew.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (ew *errorResponseWriter) Write(b []byte) (int, error) {
	if ew.statusCode >= 400 {
		// Drop original error body, we will render our own
		ew.statusMsg += string(b)
		return len(b), nil
	}
	if !ew.wroteHeader {
		ew.WriteHeader(http.StatusOK)
	}
	return ew.ResponseWriter.Write(b)
}

func (ew *errorResponseWriter) WriteHeader(code int) {
	if ew.wroteHeader {
		return
	}
	ew.statusCode = code
	if code >= 400 {
		return
	}
	ew.wroteHeader = true
	ew.ResponseWriter.WriteHeader(code)
}

type functionHandler struct {
	basePath string
	handler  func(http.ResponseWriter, *http.Request)
}

type pageRender struct {
	ph          page.PageHandler
	l10nRenders map[string]string
}
