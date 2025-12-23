package page

import (
	"fmt"
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

func (p *PageHandler) ContentToHTMLMap() map[string]string {
	items := map[string]string{}

	for slot, content := range p.Content {
		items[slot] = content.ToHTML(slot)
	}

	return items
}

func (p PageHandler) NavigationToHTMLMap() map[string]string {
	items := map[string]string{}

	for navKey := range p.Navigations {
		items[navKey] = fmt.Sprintf(navigationTemplate, navKey, navKey, p.Page.Spec.BasePath, navKey)
	}

	return items
}

func (r *PackedContent) ToHTML(slot string) string {
	if r.Content != "" {
		return fmt.Sprintf(rawHTMLTemplate, slot, r.Content)
	}

	return fmt.Sprintf(customElementTemplate, r.CustomElementName, slot, r.AppName, r.AppGeneration, r.CustomElementName)
}
