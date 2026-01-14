package host

import (
	"net/http"
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/host/ico"
	ko "kdex.dev/web/internal/openapi"
	"kdex.dev/web/internal/page"
)

const (
	kdexUIMetaTemplate = `<meta
	name="kdex-ui"
	data-page-basepath="%s"
	data-navigation-endpoint="/~/navigation/{name}/{l10n}/{basePathMinusLeadingSlash...}"
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

	defaultLanguage           string
	favicon                   *ico.Ico
	host                      *kdexv1alpha1.KDexHostSpec
	importmap                 string
	log                       logr.Logger
	mu                        sync.RWMutex
	packageReferences         []kdexv1alpha1.PackageReference
	registeredPaths           map[string]ko.PathInfo
	pathsCollectedInReconcile map[string]ko.PathInfo
	scripts                   []kdexv1alpha1.ScriptDef
	themeAssets               []kdexv1alpha1.Asset
	translationResources      map[string]kdexv1alpha1.KDexTranslationSpec
	utilityPages              map[kdexv1alpha1.KDexUtilityPageType]page.PageHandler

	Sniffer interface {
		DocsHandler(w http.ResponseWriter, r *http.Request)
		Sniff(r *http.Request) error
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
