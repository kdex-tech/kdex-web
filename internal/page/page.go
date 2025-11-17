package page

import (
	"bytes"
	"fmt"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type PageHandler struct {
	// root object
	Page *kdexv1alpha1.KDexPageBinding

	// dereferenced resources
	Archetype       *kdexv1alpha1.KDexPageArchetype
	Content         map[string]ResolvedContentEntry
	Footer          *kdexv1alpha1.KDexPageFooter
	Header          *kdexv1alpha1.KDexPageHeader
	Navigations     map[string]*kdexv1alpha1.KDexPageNavigation
	ScriptLibraries []kdexv1alpha1.KDexScriptLibrary
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

	return p.Footer.Spec.Content
}

func (p PageHandler) HeaderToHTML() string {
	if p.Header == nil {
		return ""
	}

	return p.Header.Spec.Content
}

func (p PageHandler) NavigationToHTMLMap() map[string]string {
	items := map[string]string{}

	for navKey := range p.Navigations {
		items[navKey] = fmt.Sprintf(`
<div id="navigation-%s"></div>
<script type="text/javascript">
fetch('/~/navigation/%s/{{ .Language }}%s')
  .then(response => response.text())
  .then(data => {
    document.getElementById('navigation-%s').innerHTML += data;
  });
</script>
`, navKey, navKey, p.Page.Spec.BasePath, navKey)
	}

	return items
}

type ResolvedContentEntry struct {
	App               *kdexv1alpha1.KDexApp
	Content           string
	CustomElementName string
	Slot              string
}

func (r *ResolvedContentEntry) ToHTML() string {
	if r.Content != "" {
		return fmt.Sprintf(`<div id="%s">%s</div>`, r.Slot, r.Content)
	}

	var buffer bytes.Buffer

	buffer.WriteRune('<')
	buffer.WriteString(r.CustomElementName)
	buffer.WriteString(` id="`)
	buffer.WriteString(r.Slot)
	buffer.WriteString(`" data-app-name="`)
	buffer.WriteString(r.App.Name)
	buffer.WriteString(`" data-app-resource-version="`)
	buffer.WriteString(r.App.ResourceVersion)
	buffer.WriteString(`"></`)
	buffer.WriteString(r.CustomElementName)
	buffer.WriteRune('>')

	return buffer.String()
}
