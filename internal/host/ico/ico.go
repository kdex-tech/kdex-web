package ico

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
	"text/template"

	"time"

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
	data          render.TemplateData
	template      *template.Template
	reconcileTime time.Time
}

func NewICO(svgTemplate string, data render.TemplateData) *Ico {
	if svgTemplate == "" {
		svgTemplate = svgTemplateDefault
	}

	return &Ico{
		data:          data,
		template:      template.Must(template.New("favicon").Funcs(sprig.FuncMap()).Parse(svgTemplate)),
		reconcileTime: time.Now(),
	}
}

func (i *Ico) SetReconcileTime(t time.Time) {
	i.reconcileTime = t
}

func (i *Ico) FaviconHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Check Cache
	if val, ok := faviconCache.Load("favicon"); ok {
		serveSVG(w, r, val.([]byte), i.reconcileTime)
		return
	}

	// 2. Generate SVG
	var buf bytes.Buffer
	if err := i.template.Execute(&buf, i.data); err != nil {
		log := logf.FromContext(r.Context())
		log.Error(err, "error rendering favicon template")
		http.Error(w, fmt.Sprintf("Favicon template error: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	svgContent := buf.Bytes()

	// 3. Store and Serve
	faviconCache.Store("favicon", svgContent)
	serveSVG(w, r, svgContent, i.reconcileTime)
}

func serveSVG(w http.ResponseWriter, r *http.Request, data []byte, reconcileTime time.Time) {
	lastModified := reconcileTime.UTC().Truncate(time.Second)
	etag := fmt.Sprintf(`"%d"`, lastModified.Unix())

	// Note: We serve image/svg+xml even for the .ico path
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400, must-revalidate")
	w.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
	w.Header().Set("ETag", etag)

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if ifModifiedSince := r.Header.Get("If-Modified-Since"); ifModifiedSince != "" {
		t, err := http.ParseTime(ifModifiedSince)
		if err == nil && !lastModified.After(t) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	_, err := w.Write(data)
	if err != nil {
		log := logf.FromContext(r.Context())
		log.Error(err, "failed to write favicon response")
	}
}
