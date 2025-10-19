package render

import (
	"html/template"
	"time"

	"kdex.dev/web/internal/menu"
)

type Page struct {
	Contents        map[string]string
	Footer          string
	Header          string
	Label           string
	Navigations     map[string]string
	TemplateContent string
	TemplateName    string
}

type TemplateData struct {
	Content      map[string]template.HTML
	Date         time.Time
	Footer       template.HTML
	FootScript   template.HTML
	Header       template.HTML
	HeadScript   template.HTML
	Lang         string
	MenuEntries  map[string]*menu.MenuEntry
	Meta         template.HTML
	Navigation   map[string]template.HTML
	Organization string
	Stylesheet   template.HTML
	Title        string
}
