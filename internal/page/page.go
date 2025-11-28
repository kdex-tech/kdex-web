package page

import (
	"fmt"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

const (
	customElementTemplate = `<%s id="content-%s" data-app-name="%s" data-app-generation="%s"></%s>`
	navigationTemplate    = `<div id="navigation-%s"></div>
<script type="text/javascript">
fetch('/~/navigation/%s/{{ .Language }}%s')
  .then(response => response.text())
  .then(data => {
    document.getElementById('navigation-%s').innerHTML += data;
  });
</script>`
	rawHTMLTemplate = `<div id="content-%s">%s</div>`
)

type ResolvedNavigationSpec struct {
	Generation int64
	Name       string
	Spec       *kdexv1alpha1.KDexPageNavigationSpec
}

type PageHandler struct {
	// root object
	Page *kdexv1alpha1.KDexPageBinding

	// dereferenced resources
	Archetype         *kdexv1alpha1.KDexPageArchetypeSpec
	Content           map[string]ResolvedContentEntry
	Footer            *kdexv1alpha1.KDexPageFooterSpec
	Header            *kdexv1alpha1.KDexPageHeaderSpec
	Navigations       map[string]ResolvedNavigationSpec
	PackageReferences []kdexv1alpha1.PackageReference
	ScriptLibraries   []kdexv1alpha1.KDexScriptLibrarySpec
}

func (p *PageHandler) ContentToHTMLMap() map[string]string {
	items := map[string]string{}

	for slot, content := range p.Content {
		items[slot] = content.ToHTML()
	}

	return items
}

func (p PageHandler) FooterToHTML() string {
	if p.Footer == nil {
		return ""
	}

	return p.Footer.Content
}

func (p PageHandler) HeaderToHTML() string {
	if p.Header == nil {
		return ""
	}

	return p.Header.Content
}

func (p PageHandler) NavigationToHTMLMap() map[string]string {
	items := map[string]string{}

	for navKey := range p.Navigations {
		items[navKey] = fmt.Sprintf(navigationTemplate, navKey, navKey, p.Page.Spec.BasePath, navKey)
	}

	return items
}

type ResolvedContentEntry struct {
	App               *kdexv1alpha1.KDexAppSpec
	AppName           string
	AppGeneration     string
	Content           string
	CustomElementName string
	Slot              string
}

func (r *ResolvedContentEntry) ToHTML() string {
	if r.Content != "" {
		return fmt.Sprintf(rawHTMLTemplate, r.Slot, r.Content)
	}

	return fmt.Sprintf(customElementTemplate, r.CustomElementName, r.Slot, r.AppName, r.AppGeneration, r.CustomElementName)
}
