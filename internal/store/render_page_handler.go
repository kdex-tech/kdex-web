package store

import (
	"bytes"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type RenderPageHandler struct {
	Page       kdexv1alpha1.MicroFrontEndRenderPage
	Stylesheet *kdexv1alpha1.MicroFrontEndStylesheet
}

func (h *RenderPageHandler) StylesheetToString() string {
	var styleBuffer bytes.Buffer

	if h.Stylesheet != nil {
		for _, item := range h.Stylesheet.Spec.StyleItems {
			if item.LinkHref != "" {
				styleBuffer.WriteString(`<link`)
				for key, value := range item.Attributes {
					if key == "href" || key == "src" {
						continue
					}
					styleBuffer.WriteRune(' ')
					styleBuffer.WriteString(key)
					styleBuffer.WriteString(`="`)
					styleBuffer.WriteString(value)
					styleBuffer.WriteRune('"')
				}
				styleBuffer.WriteString(` href="`)
				styleBuffer.WriteString(item.LinkHref)
				styleBuffer.WriteString(`"/>`)
				styleBuffer.WriteRune('\n')
			} else if item.Style != "" {
				styleBuffer.WriteString(`<style`)
				for key, value := range item.Attributes {
					if key == "href" || key == "src" {
						continue
					}
					styleBuffer.WriteRune(' ')
					styleBuffer.WriteString(key)
					styleBuffer.WriteString(`="`)
					styleBuffer.WriteString(value)
					styleBuffer.WriteRune('"')
				}
				styleBuffer.WriteRune('>')
				styleBuffer.WriteRune('\n')
				styleBuffer.WriteString(item.Style)
				styleBuffer.WriteString("</style>")
				styleBuffer.WriteRune('\n')
			}
		}
	}

	return styleBuffer.String()
}
