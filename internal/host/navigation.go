package host

import (
	"context"

	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/api/resource"
	"kdex.dev/crds/render"
	"kdex.dev/web/internal/page"
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
