package render

import (
	"time"

	"golang.org/x/text/message"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
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
	PageMap        *map[string]*kdexv1alpha1.PageEntry
	RenderTime     time.Time
	Stylesheet     string
}
