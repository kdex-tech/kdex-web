package ico

import (
	"fmt"
	"net/http"
	"sync"
)

var (
	// Cache stores [string][]byte
	faviconCache sync.Map
)

const svgTemplate = `
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
    <circle cx="50" cy="50" r="48" fill="#000" />
    <text x="50" y="50" 
          font-family="Arial, sans-serif" 
          font-size="60" 
          font-weight="bold"
          fill="white" 
          text-anchor="middle" 
          dominant-baseline="central">%s</text>
</svg>`

type Ico struct {
	Char string
}

func (i *Ico) FaviconHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Check Cache
	if val, ok := faviconCache.Load(i.Char); ok {
		serveSVG(w, val.([]byte))
		return
	}

	// 2. Generate SVG
	svgContent := fmt.Sprintf(svgTemplate, i.Char)
	svgBytes := []byte(svgContent)

	// 3. Store and Serve
	faviconCache.Store(i.Char, svgBytes)
	serveSVG(w, svgBytes)
}

func serveSVG(w http.ResponseWriter, data []byte) {
	// Note: We serve image/svg+xml even for the .ico path
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}
