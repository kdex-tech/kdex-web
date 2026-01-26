package host

import (
	"context"
	"net/http"
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/auth"
	"kdex.dev/web/internal/host/ico"
	ko "kdex.dev/web/internal/openapi"
	"kdex.dev/web/internal/page"
	"kdex.dev/web/internal/sniffer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kdexUIMetaTemplate = `<meta
	name="kdex-ui"
	data-page-basepath="%s"
	data-navigation-endpoint="/~/navigation/{name}/{l10n}/{basePathMinusLeadingSlash...}"
	data-openapi-endpoint="/~/openapi"
	data-page-patternpath="%s"
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
		CheckPageAccess(ctx context.Context, pageSecurity, hostSecurity *[]kdexv1alpha1.SecurityRequirement) (bool, error)
	}

	authExchanger interface {
		AuthCodeURL(state string) string
		ExchangeCode(ctx context.Context, code string) (string, error)
		ExchangeToken(ctx context.Context, rawIDToken string) (string, error)
		LoginLocal(ctx context.Context, username, password string) (string, error)
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

func (ew *errorResponseWriter) Write(b []byte) (int, error) {
	if ew.statusCode >= 400 {
		// Drop original error body, we will render our own
		ew.statusMsg = string(b)
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

type pageRender struct {
	ph          page.PageHandler
	l10nRenders map[string]string
}
