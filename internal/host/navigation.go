package host

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kdex-tech/kdex-host/internal/auth"
	kdexhttp "github.com/kdex-tech/kdex-host/internal/http"
	"github.com/kdex-tech/kdex-host/internal/page"
	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/api/resource"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
)

func (hh *HostHandler) BuildMenuEntries(
	ctx context.Context,
	entry *render.PageEntry,
	l *language.Tag,
	isDefaultLanguage bool,
	parent *page.PageHandler,
) {
	for _, handler := range hh.Pages.List() {
		ph := handler.Page

		if hh.authChecker != nil {
			access, _ := hh.authChecker.CheckAccess(ctx, "pages", ph.BasePath, hh.pageRequirements(&handler))

			if !access {
				continue
			}
		}

		if (parent == nil && ph.ParentPageRef == nil) ||
			(parent != nil && ph.ParentPageRef != nil &&
				parent.Name == ph.ParentPageRef.Name) {

			if parent != nil && parent.Name == handler.Name {
				continue
			}

			if entry.Children == nil {
				entry.Children = &map[string]any{}
			}

			label := ph.Label

			href := ph.BasePath
			if !isDefaultLanguage {
				href = "/" + l.String() + ph.BasePath
			}

			pageEntry := render.PageEntry{
				BasePath: ph.BasePath,
				Href:     href,
				Label:    label,
				Name:     handler.Name,
				Weight:   resource.MustParse("0"),
			}

			if ph.NavigationHints != nil {
				pageEntry.Icon = ph.NavigationHints.Icon
				pageEntry.Weight = ph.NavigationHints.Weight
			}

			hh.BuildMenuEntries(ctx, &pageEntry, l, isDefaultLanguage, &handler)

			(*entry.Children)[label] = pageEntry
		}
	}
}

func (hh *HostHandler) NavigationGet(w http.ResponseWriter, r *http.Request) {
	if hh.applyCachingHeaders(w, r, []kdexv1alpha1.SecurityRequirement{{"authenticated": {}}}, time.Time{}) {
		return
	}

	hh.mu.RLock()
	defer hh.mu.RUnlock()

	basePath := "/" + r.PathValue("basePathMinusLeadingSlash")
	l10n := r.PathValue("l10n")
	navKey := r.PathValue("navKey")

	userHash := hh.getUserHash(r)
	cacheKey := fmt.Sprintf("nav:%s:%s:%s:%s", navKey, basePath, l10n, userHash)
	rendered, ok, err := hh.renderCache.Get(r.Context(), cacheKey)
	if err == nil && ok {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(rendered))
		return
	}

	hh.log.V(2).Info("generating navigation", "basePath", basePath, "l10n", l10n, "navKey", navKey)

	var pageHandler *page.PageHandler

	for _, ph := range hh.Pages.List() {
		if ph.BasePath() == basePath {
			pageHandler = &ph
			break
		}
	}

	if pageHandler == nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}

	var nav string

	for key, n := range pageHandler.Navigations {
		if key == navKey {
			nav = n
			break
		}
	}

	if nav == "" {
		http.Error(w, "navigation not found", http.StatusNotFound)
		return
	}

	l, err := kdexhttp.GetLang(r, hh.defaultLanguage, hh.Translations.Languages())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Filter navigation by page access checks

	rootEntry := &render.PageEntry{}
	hh.BuildMenuEntries(r.Context(), rootEntry, &l, l.String() == hh.defaultLanguage, nil)
	var pageMap map[string]any
	if rootEntry.Children != nil {
		pageMap = *rootEntry.Children
	}

	authContext, _ := auth.GetAuthContext(r.Context())
	extra := map[string]any{}
	if authContext != nil {
		extra["Identity"] = authContext
	}

	renderer := render.Renderer{
		BasePath:        pageHandler.Page.BasePath,
		BrandName:       hh.host.BrandName,
		DefaultLanguage: hh.defaultLanguage,
		Extra:           extra,
		Language:        l.String(),
		Languages:       hh.availableLanguages(&hh.Translations),
		LastModified:    hh.reconcileTime,
		MessagePrinter:  hh.messagePrinter(&hh.Translations, l),
		Organization:    hh.host.Organization,
		PageMap:         pageMap,
		PatternPath:     pageHandler.PatternPath(),
		Title:           pageHandler.Label(),
	}

	templateData, err := renderer.TemplateData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rendered, err = renderer.RenderOne(navKey, nav, templateData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := hh.renderCache.Set(r.Context(), cacheKey, rendered); err != nil {
		hh.log.Error(err, "failed to cache navigation", "key", cacheKey)
	}

	w.Header().Set("Content-Type", "text/html")
	_, err = w.Write([]byte(rendered))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
