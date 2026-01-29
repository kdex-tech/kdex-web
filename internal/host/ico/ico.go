package ico

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"kdex.dev/crds/render"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	// Cache stores [string][]byte
	faviconCache sync.Map
)

const svgTemplateDefault = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
    <circle cx="50" cy="50" r="48" fill="#000" />
    <text x="50" y="50" 
          font-family="Arial, sans-serif" 
          font-size="60" 
          font-weight="bold"
          fill="white" 
          text-anchor="middle" 
          dominant-baseline="central">{{ .BrandName | trunc 1 }}</text>
</svg>`

type Ico struct {
	data     render.TemplateData
	template *template.Template
}

func NewICO(svgTemplate string, data render.TemplateData) *Ico {
	if svgTemplate == "" {
		svgTemplate = svgTemplateDefault
	}

	return &Ico{
		data:     data,
		template: template.Must(template.New("favicon").Funcs(sprig.FuncMap()).Parse(svgTemplate)),
	}
}

func (i *Ico) FaviconHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Check Cache
	if val, ok := faviconCache.Load("favicon"); ok {
		serveSVG(w, val.([]byte))
		return
	}

	// 2. Generate SVG
	var buf bytes.Buffer
	if err := i.template.Execute(&buf, i.data); err != nil {
		log := logf.FromContext(r.Context())
		log.Error(err, "error rendering favicon template")
		http.Error(w, fmt.Sprintf("Favicon template error: %s", err.Error()), 500)
		return
	}
	svgContent := buf.Bytes()

	// 3. Store and Serve
	faviconCache.Store("favicon", svgContent)
	serveSVG(w, svgContent)
}

func serveSVG(w http.ResponseWriter, data []byte) {
	// Note: We serve image/svg+xml even for the .ico path
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, err := w.Write(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
