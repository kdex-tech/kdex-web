package render

import (
	"bytes"
	"fmt"
	"html/template"
	"time"

	"golang.org/x/text/message"
	"kdex.dev/web/internal/menu"
)

type Renderer struct {
	Date           time.Time
	FootScript     string
	HeadScript     string
	Lang           string
	MenuEntries    *map[string]*menu.MenuEntry
	MessagePrinter *message.Printer
	Meta           string
	Organization   string
	Stylesheet     string
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
		Date:         date,
		FootScript:   template.HTML(r.FootScript),
		HeadScript:   template.HTML(r.HeadScript),
		Lang:         r.Lang,
		MenuEntries:  *menuEntries,
		Meta:         template.HTML(r.Meta),
		Organization: r.Organization,
		Stylesheet:   template.HTML(r.Stylesheet),
		Title:        page.Label,
	}

	headerOutput, err := r.RenderOne(fmt.Sprintf("%s-header", page.TemplateName), page.Header, templateData)
	if err != nil {
		return "", err
	}

	templateData.Header = template.HTML(headerOutput)

	footerOutput, err := r.RenderOne(fmt.Sprintf("%s-footer", page.TemplateName), page.Footer, templateData)
	if err != nil {
		return "", err
	}

	templateData.Footer = template.HTML(footerOutput)

	navigationOutputs := make(map[string]template.HTML)
	for name, content := range page.Navigations {
		output, err := r.RenderOne(fmt.Sprintf("%s-navigation-%s", page.TemplateName, name), content, templateData)
		if err != nil {
			return "", err
		}
		navigationOutputs[name] = template.HTML(output)
	}

	templateData.Navigation = navigationOutputs

	contentOutputs := make(map[string]template.HTML)
	for slot, content := range page.Contents {
		output, err := r.RenderOne(fmt.Sprintf("%s-content-%s", page.TemplateName, slot), content, templateData)
		if err != nil {
			return "", err
		}

		contentOutputs[slot] = template.HTML(output)
	}

	templateData.Content = contentOutputs

	return r.RenderOne(page.TemplateName, page.TemplateContent, templateData)
}

func (r *Renderer) RenderOne(
	templateName string,
	templateContent string,
	data TemplateData,
) (string, error) {
	funcs := template.FuncMap{
		"l10n": func(key string, args ...string) string {
			if r.MessagePrinter == nil {
				return key
			}
			return r.MessagePrinter.Sprintf(key, args)
		},
	}

	instance, err := template.New(templateName).Funcs(funcs).Parse(templateContent)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := instance.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
