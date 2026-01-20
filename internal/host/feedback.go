package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	ko "kdex.dev/web/internal/openapi"
	"kdex.dev/web/internal/sniffer"
)

type FeedbackTheme struct {
	// CLI Colors (ANSI)
	CLIHeader  string
	CLISuccess string
	CLIWarning string
	CLIDim     string
	CLILineNum string
	CLIReset   string

	// HTML Colors (CSS)
	BgPage        string
	BgSidebar     string
	BgCard        string
	BgCode        string
	Border        string
	TextPrimary   string
	TextSecondary string
	TextAccent    string
	TextLint      string
	TextCode      string
	MethodGet     string
	MethodPost    string
	MethodPut     string
	MethodDelete  string
	BtnSuccess    string
	BtnHover      string
}

var defaultTheme = FeedbackTheme{
	CLIHeader:  "\033[1;36m",
	CLISuccess: "\033[1;32m",
	CLIWarning: "\033[1;33m",
	CLIDim:     "\033[2m",
	CLILineNum: "\033[90m",
	CLIReset:   "\033[0m",

	BgPage:        "#0d1117",
	BgSidebar:     "#161b22",
	BgCard:        "#21262d",
	BgCode:        "#1e1e1e",
	Border:        "#30363d",
	TextPrimary:   "#c9d1d9",
	TextSecondary: "#8b949e",
	TextAccent:    "#58a6ff",
	TextLint:      "#d29922",
	TextCode:      "#9cdcfe",
	MethodGet:     "#238636",
	MethodPost:    "#1f6feb",
	MethodPut:     "#9e6a03",
	MethodDelete:  "#da3633",
	BtnSuccess:    "#238636",
	BtnHover:      "#2ea043",
}

// AnalysisCache stores the results of the InferenceEngine for a short period
// so that the redirected user can view the report.
type AnalysisCache struct {
	entries sync.Map
}

type cachedAnalysis struct {
	Result    *sniffer.AnalysisResult
	Timestamp time.Time
}

func NewAnalysisCache() *AnalysisCache {
	ac := &AnalysisCache{}
	go ac.reap()
	return ac
}

func (ac *AnalysisCache) Store(result *sniffer.AnalysisResult) string {
	id := uuid.New().String()
	ac.entries.Store(id, cachedAnalysis{
		Result:    result,
		Timestamp: time.Now(),
	})
	return id
}

func (ac *AnalysisCache) Get(id string) (*sniffer.AnalysisResult, bool) {
	val, ok := ac.entries.Load(id)
	if !ok {
		return nil, false
	}
	return val.(cachedAnalysis).Result, true
}

func (ac *AnalysisCache) reap() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		ac.entries.Range(func(key, value any) bool {
			entry := value.(cachedAnalysis)
			if now.Sub(entry.Timestamp) > 10*time.Minute {
				ac.entries.Delete(key)
			}
			return true
		})
	}
}

// User-Agent detection for CLI tools
func isCLI(userAgent string) bool {
	userAgent = strings.ToLower(userAgent)
	return strings.Contains(userAgent, "curl") ||
		strings.Contains(userAgent, "wget") ||
		strings.Contains(userAgent, "httpie")
}

func (th *HostHandler) DesignMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only intercept if we have a sniffer (checker) and it's not an internal path
		if th.sniffer == nil || strings.HasPrefix(r.URL.Path, "/~") {
			next.ServeHTTP(w, r)
			return
		}

		// Body Persistence: Read body so we can restore it for the next handler AND the sniffer
		var bodyBytes []byte
		if r.Body != nil {
			bodyBytes, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// Create a wrapper to capture the status code
		ew := &errorResponseWriter{ResponseWriter: w}
		next.ServeHTTP(ew, r)

		// If it was a 404, we hijack the response
		if ew.statusCode == http.StatusNotFound {
			// Restore body for analysis
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// Analyze
			result, err := th.sniffer.Analyze(r)
			if err != nil {
				th.log.Error(err, "failed to analyze request", "path", r.URL.Path)
				// Fallback to standard error serving if analysis fails
				th.serveError(w, r, http.StatusBadRequest, err.Error())
				return
			}

			if result.Function == nil {
				// Analysis yielded nothing (maybe pattern mismatch), serve 404 as usual
				th.serveError(w, r, ew.statusCode, ew.statusMsg)
				return
			}

			// Store result
			id := th.analysisCache.Store(result)

			// Smart Redirection
			format := "html"
			if isCLI(r.UserAgent()) || strings.Contains(r.Header.Get("Accept"), "text/plain") {
				format = "text"
			}

			absoluteURL := fmt.Sprintf("%s/inspect/%s?format=%s", ko.Host(r), id, format)
			inspectURL := fmt.Sprintf("/inspect/%s?format=%s", id, format)

			w.Header().Set("Location", inspectURL)
			w.Header().Set("X-KDex-Sniffer-Docs", "/~/sniffer/docs")
			w.WriteHeader(http.StatusSeeOther)

			// Fallback body for those who don't follow redirects
			// Use OSC 8 for clickable link
			link := fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", absoluteURL, absoluteURL)
			fmt.Fprintf(w, "➔ API Draft Created. View at: %s\n(Note: Use 'curl -L' to follow automatically).\n", link)
			return
		}

		// If we didn't hijack 404, we must ensure the status code from next handler is written if it wasn't already.
		// Our errorResponseWriter captures WriteHeader(code) for code >= 400.
		// If code < 400, it passed through immediately.
		// If code >= 400, it buffered it.
		if ew.statusCode >= 400 {
			// Write the buffered status code structure
			th.serveError(w, r, ew.statusCode, ew.statusMsg)
		}
	})
}

// InspectHandler serves the feedback UI
func (th *HostHandler) InspectHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("uuid")
	format := r.URL.Query().Get("format")

	result, ok := th.analysisCache.Get(id)
	if !ok {
		http.Error(w, "Analysis result expired or not found.", http.StatusNotFound)
		return
	}

	// Generate OpenAPI spec snippet
	spec := ko.BuildOneOff(ko.Host(r), result.Function)
	specBytes, _ := json.MarshalIndent(spec, "", "  ")
	specStr := string(specBytes)

	if format == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(fmt.Sprintf("%s─── API DESIGN FEEDBACK ───%s\n\n", defaultTheme.CLIHeader, defaultTheme.CLIReset)))

		w.Write([]byte(fmt.Sprintf("%s✓ Analyzed Request:%s %s %s\n", defaultTheme.CLISuccess, defaultTheme.CLIReset, result.OriginalRequest.Method, result.OriginalRequest.URL.Path)))

		if len(result.Lints) > 0 {
			w.Write([]byte(fmt.Sprintf("\n%sWarnings / Insights:%s\n", defaultTheme.CLIWarning, defaultTheme.CLIReset)))
			for _, lint := range result.Lints {
				w.Write([]byte(fmt.Sprintf("  • %s\n", lint)))
			}
		}

		w.Write([]byte(fmt.Sprintf("\n%sGenerated OpenAPI Spec (Fragment):%s\n", defaultTheme.CLIDim, defaultTheme.CLIReset)))
		lines := strings.Split(specStr, "\n")
		for i, line := range lines {
			if line == "" && i == len(lines)-1 {
				break
			}
			w.Write([]byte(fmt.Sprintf("%s%4d │ %s%s\n", defaultTheme.CLILineNum, i+1, defaultTheme.CLIReset, line)))
		}
		return
	}

	// HTML Dashboard
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>KDex API Workbench</title>
	<style>
		body { margin: 0; font-family: 'Inter', system-ui, sans-serif; background: %[1]s; color: %[2]s; display: grid; grid-template-columns: 350px 1fr; height: 100vh; overflow: hidden; }
		.sidebar { background: %[3]s; border-right: 1px solid %[4]s; padding: 20px; overflow-y: auto; }
		.main { padding: 20px; overflow-y: auto; display: flex; flex-direction: column; }
		h1 { font-size: 16px; margin: 0 0 20px; color: %[5]s; font-weight: 600; text-transform: uppercase; letter-spacing: 1px; }
		h2 { font-size: 14px; margin: 20px 0 10px; color: %[6]s; border-bottom: 1px solid %[4]s; padding-bottom: 5px; }
		.card { background: %[7]s; border: 1px solid %[4]s; border-radius: 6px; padding: 15px; margin-bottom: 15px; }
		.method { display: inline-block; padding: 2px 6px; border-radius: 4px; font-weight: bold; font-size: 12px; margin-right: 8px; }
		.method.GET { background: %[8]s; color: white; }
		.method.POST { background: %[9]s; color: white; }
		.method.PUT { background: %[10]s; color: white; }
		.method.DELETE { background: %[11]s; color: white; }
		.lint-item { margin-bottom: 8px; font-size: 13px; display: flex; gap: 8px; align-items: flex-start; }
		.lint-icon { color: %[12]s; }
		pre { margin: 0; font-family: 'JetBrains Mono', monospace; font-size: 13px; }
		code { display: block; padding: 15px; background: %[13]s; color: %[14]s; border-radius: 6px; overflow-x: auto; box-shadow: 0 4px 12px rgba(0,0,0,0.3); }
		.ln { color: %[5]s; opacity: 0.5; margin-right: 15px; user-select: none; border-right: 1px solid %[4]s; padding-right: 10px; display: inline-block; min-width: 30px; text-align: right; }
		.lc { color: %[14]s; }
		.toolbar { display: flex; justify-content: flex-end; margin-bottom: 10px; }
		button { background: %[15]s; color: white; border: none; padding: 6px 12px; border-radius: 6px; font-weight: 600; cursor: pointer; transition: background 0.2s; }
		button:hover { background: %[16]s; }
	</style>
</head>
<body>
	<div class="sidebar">
		<h1>API Workbench</h1>
		
		<div class="card">
			<div style="font-size: 12px; color: %[6]s; margin-bottom: 4px;">Request Invariants</div>
			<div style="font-family: monospace; font-size: 14px;">
				<span class="method %[17]s">%[17]s</span>
				<span title="%[18]s">%[18]s</span>
			</div>
		</div>

		<h2>Analysis & Linting</h2>
		%[19]s
	</div>
	<div class="main">
		<div class="toolbar">
			<button onclick="navigator.clipboard.writeText(document.querySelector('code').innerText); this.innerText='Copied!'">Copy Spec Fragment</button>
		</div>
		<pre><code>%[20]s</code></pre>
	</div>
</body>
</html>`,
		defaultTheme.BgPage,             // 1
		defaultTheme.TextPrimary,        // 2
		defaultTheme.BgSidebar,          // 3
		defaultTheme.Border,             // 4
		defaultTheme.TextAccent,         // 5
		defaultTheme.TextSecondary,      // 6
		defaultTheme.BgCard,             // 7
		defaultTheme.MethodGet,          // 8
		defaultTheme.MethodPost,         // 9
		defaultTheme.MethodPut,          // 10
		defaultTheme.MethodDelete,       // 11
		defaultTheme.TextLint,           // 12
		defaultTheme.BgCode,             // 13
		defaultTheme.TextCode,           // 14
		defaultTheme.BtnSuccess,         // 15
		defaultTheme.BtnHover,           // 16
		result.OriginalRequest.Method,   // 17
		result.OriginalRequest.URL.Path, // 18
		generateLintHTML(result.Lints),  // 19
		renderSpecHTML(specStr),         // 20
	)

	w.Write([]byte(html))
}

func generateLintHTML(lints []string) string {
	if len(lints) == 0 {
		return fmt.Sprintf(`<div style="font-size: 13px; color: %s; font-style: italic;">No linting issues found.</div>`, defaultTheme.TextSecondary)
	}
	var b strings.Builder
	for _, l := range lints {
		b.WriteString(fmt.Sprintf(`<div class="lint-item"><span class="lint-icon" style="color: %s">⚠</span> <span>%s</span></div>`, defaultTheme.TextLint, htmlEscape(l)))
	}
	return b.String()
}

func htmlEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "&", "&amp;"), "<", "&lt;")
}

func renderSpecHTML(spec string) string {
	lines := strings.Split(spec, "\n")
	var b strings.Builder
	for i, line := range lines {
		if line == "" && i == len(lines)-1 {
			break
		}
		// We use a separate span for the line number and the content
		b.WriteString(fmt.Sprintf(`<span class="ln">%d</span><span class="lc">%s</span>`+"\n", i+1, htmlEscape(line)))
	}
	return b.String()
}
