package render

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"kdex.dev/web/internal/menu"
)

type Renderer struct {
	Context      context.Context
	Date         time.Time
	FootScript   string
	HeadScript   string
	Lang         string
	MenuEntries  *map[string]*menu.MenuEntry
	Meta         string
	Organization string
	Request      *http.Request
	Stylesheet   string
}

func (r *Renderer) RenderPage(page Page) (string, error) {
	date := r.Date
	if date.IsZero() {
		date = time.Now()
	}

	menuEntries := &map[string]*menu.MenuEntry{}
	if r.MenuEntries != nil {
		menuEntries = r.MenuEntries
	}

	templateData := TemplateData{
		Values: Values{
			Date:         date,
			FootScript:   template.HTML(r.FootScript),
			HeadScript:   template.HTML(r.HeadScript),
			Lang:         r.Lang,
			MenuEntries:  *menuEntries,
			Meta:         template.HTML(r.Meta),
			Organization: r.Organization,
			Stylesheet:   template.HTML(r.Stylesheet),
			Title:        page.Label,
		},
	}

	headerOutput, err := r.RenderOne(fmt.Sprintf("%s-header", page.TemplateName), page.Header, templateData)
	if err != nil {
		return "", err
	}

	templateData.Values.Header = template.HTML(headerOutput)

	footerOutput, err := r.RenderOne(fmt.Sprintf("%s-footer", page.TemplateName), page.Footer, templateData)
	if err != nil {
		return "", err
	}

	templateData.Values.Footer = template.HTML(footerOutput)

	navigationOutputs := make(map[string]template.HTML)
	for name, content := range page.Navigations {
		output, err := r.RenderOne(fmt.Sprintf("%s-navigation-%s", page.TemplateName, name), content, templateData)
		if err != nil {
			return "", err
		}
		navigationOutputs[name] = template.HTML(output)
	}

	templateData.Values.Navigation = navigationOutputs

	contentOutputs := make(map[string]template.HTML)
	for slot, content := range page.Contents {
		output, err := r.RenderOne(fmt.Sprintf("%s-content-%s", page.TemplateName, slot), content, templateData)
		if err != nil {
			return "", err
		}

		contentOutputs[slot] = template.HTML(output)
	}

	templateData.Values.Content = contentOutputs

	return r.RenderOne(page.TemplateName, page.TemplateContent, templateData)
}

func (r *Renderer) RenderOne(templateName string, templateContent string, data any) (string, error) {
	instance, err := template.New(templateName).Parse(templateContent)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := instance.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
