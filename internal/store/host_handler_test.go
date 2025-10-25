package store

import (
	"testing"

	"github.com/go-logr/logr"
	G "github.com/onsi/gomega"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
)

const (
	primaryTemplate = `<!DOCTYPE html>
<html lang="{{ .Language }}">
	<head>
	{{ .Meta }}
	{{ .Title }}
	{{ .Stylesheet }}
	{{ .HeadScript }}
	</head>
	<body>
	<header>
		{{ .Header }}
	</header>
	<nav>
		{{ .Navigation.main }}
	</nav>
	<main>
		{{ .Content.main }}
	</main>
	<footer>
		{{ .Footer }}
	</footer>
	{{ .FootScript }}
	</body>
</html>`
)

func TestHostHandler_L10nRenderLocked(t *testing.T) {
	tests := []struct {
		name         string
		host         kdexv1alpha1.MicroFrontEndHost
		page         kdexv1alpha1.MicroFrontEndRenderPage
		lang         string
		translations *catalog.Builder
		want         []string
	}{
		{
			name: "english translation",
			host: kdexv1alpha1.MicroFrontEndHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.MicroFrontEndHostSpec{
					AppPolicy:    kdexv1alpha1.NonStrictAppPolicy,
					DefaultLang:  "en",
					Domains:      []string{"foo.bar"},
					Organization: "KDex Tech Inc.",
				},
			},
			page: kdexv1alpha1.MicroFrontEndRenderPage{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-render-page",
				},
				Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
					PageComponents: kdexv1alpha1.PageComponents{
						Contents: map[string]string{
							"main": "MAIN",
						},
						Footer: "FOOTER",
						Header: `{{ l10n "key" }}`,
						Navigations: map[string]string{
							"main": "NAV",
						},
						PrimaryTemplate: primaryTemplate,
						Title:           "TITLE",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			},
			lang: "en",
			translations: func() *catalog.Builder {
				b := catalog.NewBuilder()
				b.SetString(language.English, "key", "ENGLISH_TRANSLATION")
				b.SetString(language.French, "key", "FRENCH_TRANSLATION")
				return b
			}(),
			want: []string{"FOOTER", "ENGLISH_TRANSLATION", "NAV", "MAIN", "TITLE"},
		},
		{
			name: "english translation",
			host: kdexv1alpha1.MicroFrontEndHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.MicroFrontEndHostSpec{
					AppPolicy:    kdexv1alpha1.NonStrictAppPolicy,
					DefaultLang:  "en",
					Domains:      []string{"foo.bar"},
					Organization: "KDex Tech Inc.",
				},
			},
			page: kdexv1alpha1.MicroFrontEndRenderPage{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-render-page",
				},
				Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
					PageComponents: kdexv1alpha1.PageComponents{
						Contents: map[string]string{
							"main": "MAIN",
						},
						Footer: "FOOTER",
						Header: `{{ l10n "key" }}`,
						Navigations: map[string]string{
							"main": "NAV",
						},
						PrimaryTemplate: primaryTemplate,
						Title:           "TITLE",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			},
			lang: "fr",
			translations: func() *catalog.Builder {
				b := catalog.NewBuilder()
				b.SetString(language.English, "key", "ENGLISH_TRANSLATION")
				b.SetString(language.French, "key", "FRENCH_TRANSLATION")
				return b
			}(),
			want: []string{"FOOTER", "FRENCH_TRANSLATION", "NAV", "MAIN", "TITLE"},
		},
		{
			name: "no translation",
			host: kdexv1alpha1.MicroFrontEndHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.MicroFrontEndHostSpec{
					AppPolicy:    kdexv1alpha1.NonStrictAppPolicy,
					DefaultLang:  "en",
					Domains:      []string{"foo.bar"},
					Organization: "KDex Tech Inc.",
				},
			},
			page: kdexv1alpha1.MicroFrontEndRenderPage{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-render-page",
				},
				Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
					PageComponents: kdexv1alpha1.PageComponents{
						Contents: map[string]string{
							"main": "MAIN",
						},
						Footer: "FOOTER",
						Header: `{{ l10n "key" }}`,
						Navigations: map[string]string{
							"main": "NAV",
						},
						PrimaryTemplate: primaryTemplate,
						Title:           "TITLE",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			},
			lang: "en",
			want: []string{"FOOTER", "key", "NAV", "MAIN", "TITLE"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := G.NewGomegaWithT(t)

			th := NewHostHandler(tt.host, logr.Discard())
			th.SetTranslations(tt.translations)
			got, gotErr := th.L10nRenderLocked(RenderPageHandler{
				Page: tt.page,
			}, &map[string]*render.PageEntry{}, language.Make(tt.lang))

			g.Expect(gotErr).NotTo(G.HaveOccurred())

			for _, want := range tt.want {
				g.Expect(got).To(G.ContainSubstring(want))
			}
		})
	}
}

func TestHostHandler_L10nRendersLocked(t *testing.T) {
	tests := []struct {
		name         string
		host         kdexv1alpha1.MicroFrontEndHost
		translations *catalog.Builder
		page         kdexv1alpha1.MicroFrontEndRenderPage
		want         map[string][]string
	}{
		{
			name: "translations",
			host: kdexv1alpha1.MicroFrontEndHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.MicroFrontEndHostSpec{
					AppPolicy:    kdexv1alpha1.NonStrictAppPolicy,
					DefaultLang:  "en",
					Domains:      []string{"foo.bar"},
					Organization: "KDex Tech Inc.",
				},
			},
			page: kdexv1alpha1.MicroFrontEndRenderPage{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-render-page",
				},
				Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
					PageComponents: kdexv1alpha1.PageComponents{
						Contents: map[string]string{
							"main": "MAIN",
						},
						Footer: "FOOTER",
						Header: `{{ l10n "key" }}`,
						Navigations: map[string]string{
							"main": "NAV",
						},
						PrimaryTemplate: primaryTemplate,
						Title:           "TITLE",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			},
			translations: func() *catalog.Builder {
				b := catalog.NewBuilder()
				b.SetString(language.English, "key", "ENGLISH_TRANSLATION")
				b.SetString(language.French, "key", "FRENCH_TRANSLATION")
				return b
			}(),
			want: map[string][]string{
				"en": {
					"FOOTER", "ENGLISH_TRANSLATION", "NAV", "MAIN", "TITLE",
				},
				"fr": {
					"FOOTER", "FRENCH_TRANSLATION", "NAV", "MAIN", "TITLE",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := G.NewGomegaWithT(t)

			th := NewHostHandler(tt.host, logr.Discard())
			th.SetTranslations(tt.translations)
			got := th.L10nRendersLocked(RenderPageHandler{
				Page: tt.page,
			}, map[language.Tag]*map[string]*render.PageEntry{})

			for key, values := range tt.want {
				l10nRender := got[key]
				for _, v := range values {
					g.Expect(l10nRender).To(G.ContainSubstring(v))
				}
			}
		})
	}
}

func TestHostHandler_AddOrUpdateTranslation(t *testing.T) {
	type KeyAndExpected struct {
		key      string
		expected string
	}
	tests := []struct {
		name        string
		host        kdexv1alpha1.MicroFrontEndHost
		translation kdexv1alpha1.MicroFrontEndTranslation
		langTests   map[string]KeyAndExpected
	}{
		{
			name: "add translation",
			host: kdexv1alpha1.MicroFrontEndHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.MicroFrontEndHostSpec{
					AppPolicy:    kdexv1alpha1.NonStrictAppPolicy,
					DefaultLang:  "en",
					Domains:      []string{"foo.bar"},
					Organization: "KDex Tech Inc.",
				},
			},
			translation: kdexv1alpha1.MicroFrontEndTranslation{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-translation",
				},
				Spec: kdexv1alpha1.MicroFrontEndTranslationSpec{
					Translations: []kdexv1alpha1.Translation{
						{
							Lang: "en",
							KeysAndValues: map[string]string{
								"key": "ENGLISH_TRANSLATION",
							},
						},
						{
							Lang: "fr",
							KeysAndValues: map[string]string{
								"key": "FRENCH_TRANSLATION",
							},
						},
						{
							Lang: "fr",
							KeysAndValues: map[string]string{
								"key": "LAST_ONE_WINS",
							},
						},
					},
				},
			},
			langTests: map[string]KeyAndExpected{
				"en": {
					key:      "key",
					expected: "ENGLISH_TRANSLATION",
				},
				"fr": {
					key:      "key",
					expected: "LAST_ONE_WINS",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := G.NewGomegaWithT(t)

			th := NewHostHandler(tt.host, logr.Discard())
			th.AddOrUpdateTranslation(tt.translation)

			for lang, expected := range tt.langTests {
				messagePrinter := message.NewPrinter(
					language.Make(lang),
					message.Catalog(th.Translations),
				)
				g.Expect(
					messagePrinter.Sprintf(expected.key),
				).To(G.Equal(expected.expected))
			}
		})
	}
}
