package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
	"golang.org/x/text/language"
)

func TestGetParam(t *testing.T) {
	tests := []struct {
		headers        *map[string]string
		name           string
		parameterNames []string
		path           string
		pattern        string
		supportedLangs *[]language.Tag
		want           Results
	}{
		{
			name:           "from path",
			parameterNames: []string{"lang", "path"},
			path:           "/one/two/three",
			pattern:        "/{lang}/{path...}",
			want: Results{
				Lang: "en",
				Parameters: map[string][]string{
					"lang": {"one"},
					"path": {"two/three"},
				},
			},
		},
		{
			name:           "from path, wrong param name",
			parameterNames: []string{"lang", "path"},
			path:           "/one/two/three",
			pattern:        "/{lang}/{foo...}",
			want: Results{
				Lang: "en",
				Parameters: map[string][]string{
					"lang": {"one"},
				},
			},
		},
		{
			name:           "from query",
			parameterNames: []string{"lang", "path"},
			path:           "/path?lang=one&path=two&path=three",
			pattern:        "/path",
			want: Results{
				Lang: "en",
				Parameters: map[string][]string{
					"lang": {"one"},
					"path": {"two", "three"},
				},
			},
		},
		{
			name:           "from both",
			parameterNames: []string{"lang", "path"},
			path:           "/one?path=two&path=three",
			pattern:        "/{lang}",
			want: Results{
				Lang: "en",
				Parameters: map[string][]string{
					"lang": {"one"},
					"path": {"two", "three"},
				},
			},
		},
		{
			name:           "get lang from path",
			parameterNames: []string{},
			path:           "/fr/one",
			pattern:        "/{l10n}/{path...}",
			supportedLangs: &[]language.Tag{
				language.Make("en"),
				language.Make("fr"),
			},
			want: Results{
				Lang: "fr",
			},
		},
		{
			name:           "get lang from query",
			parameterNames: []string{},
			path:           "/one?l10n=fr",
			pattern:        "/{path...}",
			supportedLangs: &[]language.Tag{
				language.Make("en"),
				language.Make("fr"),
			},
			want: Results{
				Lang: "fr",
			},
		},
		{
			name: "get lang from headers",
			headers: &map[string]string{
				"Accept-Language": "zh,fr;q=0.9,en;q=0.8",
			},
			parameterNames: []string{},
			path:           "/one",
			pattern:        "/{path...}",
			supportedLangs: &[]language.Tag{
				language.Make("en"),
				language.Make("fr"),
			},
			want: Results{
				Lang: "fr",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			got := setupHandler(
				g, tt.pattern, tt.parameterNames, tt.path, tt.supportedLangs, tt.headers)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

type Results struct {
	Lang       string              `json:"lang"`
	Parameters map[string][]string `json:"parameters,omitempty"`
}

func setupHandler(
	g *GomegaWithT,
	path string,
	parameterNames []string,
	url string,
	supportedLangs *[]language.Tag,
	headers *map[string]string,
) Results {
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		results := Results{}
		for _, name := range parameterNames {
			values := GetParamArray(name, []string{}, r)
			if len(values) > 0 {
				if results.Parameters == nil {
					results.Parameters = map[string][]string{}
				}
				results.Parameters[name] = values
			}
		}

		if supportedLangs == nil {
			supportedLangs = &[]language.Tag{
				language.Make("en"),
			}
		}

		lang := GetLang(r, "en", *supportedLangs)

		results.Lang = lang.String()

		jsonBytes, err := json.Marshal(results)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL+url, nil)
	g.Expect(err).NotTo(HaveOccurred())

	if headers != nil {
		for key, value := range *headers {
			req.Header.Add(key, value)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))

	body, err := io.ReadAll(resp.Body)
	g.Expect(err).NotTo(HaveOccurred())

	results := Results{}
	err = json.Unmarshal(body, &results)
	g.Expect(err).NotTo(HaveOccurred())

	return results
}
