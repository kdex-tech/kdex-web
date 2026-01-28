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
		page := handler.Page

		if hh.authChecker != nil {
			access, _ := hh.authChecker.CheckAccess(ctx, "pages", page.BasePath, hh.pageRequirements(&handler))

			if !access {
				continue
			}
		}

		if (parent == nil && page.ParentPageRef == nil) ||
			(parent != nil && page.ParentPageRef != nil &&
				parent.Name == page.ParentPageRef.Name) {

			if parent != nil && parent.Name == handler.Name {
				continue
			}

			if entry.Children == nil {
				entry.Children = &map[string]any{}
			}

			label := page.Label

			href := page.BasePath
			if !isDefaultLanguage {
				href = "/" + l.String() + page.BasePath
			}

			pageEntry := render.PageEntry{
				BasePath: page.BasePath,
				Href:     href,
				Label:    label,
				Name:     handler.Name,
				Weight:   resource.MustParse("0"),
			}

			if page.NavigationHints != nil {
				pageEntry.Icon = page.NavigationHints.Icon
				pageEntry.Weight = page.NavigationHints.Weight
			}

			hh.BuildMenuEntries(ctx, &pageEntry, l, isDefaultLanguage, &handler)

			(*entry.Children)[label] = pageEntry
		}
	}
}
