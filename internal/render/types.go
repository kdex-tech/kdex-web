package render

import (
	"time"

	"golang.org/x/text/message"
	kdextemplate "kdex.dev/crds/api/template"
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

type Renderer struct {
	FootScript     string
	HeadScript     string
	Language       string
	Languages      []string
	MessagePrinter *message.Printer
	Meta           string
	Organization   string
	PageMap        *map[string]*kdextemplate.PageEntry
	RenderTime     time.Time
	Stylesheet     string
}
