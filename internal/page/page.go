package page

import (
	"fmt"
	"strings"
)

const (
	customElementTemplate = `<%s id="content-%s" data-app-name="%s" data-app-generation="%s"%s></%s>`
	navigationTemplate    = `<nav id="navigation-%s">
<script type="text/javascript">
fetch('/-/navigation/%s/[[ .Language ]]%s')
  .then(response => response.text())
  .then(data => {
    document.getElementById('navigation-%s').innerHTML = data;
  });
</script>
</nav>`
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
		items[navKey] = fmt.Sprintf(navigationTemplate, navKey, navKey, p.BasePath(), navKey)
	}

	return items
}

func (p PageHandler) BasePath() string {
	if p.Page == nil {
		return ""
	}
	return p.Page.BasePath
}

func (p PageHandler) Label() string {
	if p.Page == nil {
		return ""
	}
	return p.Page.Label
}

func (p PageHandler) PatternPath() string {
	if p.Page == nil {
		return ""
	}
	return p.Page.PatternPath
}

func (r *PackedContent) ToHTML(slot string) string {
	if r.Content != "" {
		return fmt.Sprintf(rawHTMLTemplate, slot, r.Content)
	}

	var attributes strings.Builder
	for k, v := range r.Attributes {
		fmt.Fprintf(&attributes, ` %s="%s"`, k, v)
	}

	return fmt.Sprintf(customElementTemplate, r.CustomElementName, slot, r.AppName, r.AppGeneration, attributes.String(), r.CustomElementName)
}
