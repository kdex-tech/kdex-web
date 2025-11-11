package store

import (
	"testing"

	"github.com/go-logr/logr"
	G "github.com/onsi/gomega"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
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
	{{ .Theme }}
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
		host        kdexv1alpha1.KDexHost
		lang        string
		name        string
		page        kdexv1alpha1.KDexPageBinding
		translation *kdexv1alpha1.KDexTranslation
		want        []string
	}{
		{
			name: "english translation",
			host: kdexv1alpha1.KDexHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.KDexHostSpec{
					DefaultLang:  "en",
					ModulePolicy: kdexv1alpha1.LooseModulePolicy,
					Organization: "KDex Tech Inc.",
					Routing: kdexv1alpha1.Routing{
						Domains: []string{"foo.bar"},
					},
				},
			},
			page: kdexv1alpha1.KDexPageBinding{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-page-binding",
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "MAIN",
							Slot:    "main",
						},
					},
					// PageComponents: kdexv1alpha1.PageComponents{
					// 	Footer: "FOOTER",
					// 	Header: `{{ l10n "key" }}`,
					// 	Navigations: map[string]string{
					// 		"main": "NAV",
					// 	},
					// 	PrimaryTemplate: primaryTemplate,
					// 	Title:           "TITLE",
					// },
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			},
			lang: "en",
			translation: &kdexv1alpha1.KDexTranslation{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-translation",
					Namespace: "foo",
				},
				Spec: kdexv1alpha1.KDexTranslationSpec{
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
					},
				},
			},
			want: []string{"FOOTER", "ENGLISH_TRANSLATION", "NAV", "MAIN", "TITLE"},
		},
		{
			name: "english translation",
			host: kdexv1alpha1.KDexHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.KDexHostSpec{
					DefaultLang:  "en",
					ModulePolicy: kdexv1alpha1.LooseModulePolicy,
					Organization: "KDex Tech Inc.",
					Routing: kdexv1alpha1.Routing{
						Domains: []string{"foo.bar"},
					},
				},
			},
			page: kdexv1alpha1.KDexPageBinding{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-render-page",
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					// PageComponents: kdexv1alpha1.PageComponents{
					// 	Contents: map[string]string{
					// 		"main": "MAIN",
					// 	},
					// 	Footer: "FOOTER",
					// 	Header: `{{ l10n "key" }}`,
					// 	Navigations: map[string]string{
					// 		"main": "NAV",
					// 	},
					// 	PrimaryTemplate: primaryTemplate,
					// 	Title:           "TITLE",
					// },
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			},
			lang: "fr",
			translation: &kdexv1alpha1.KDexTranslation{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-translation",
					Namespace: "foo",
				},
				Spec: kdexv1alpha1.KDexTranslationSpec{
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
					},
				},
			},
			want: []string{"FOOTER", "FRENCH_TRANSLATION", "NAV", "MAIN", "TITLE"},
		},
		{
			name: "no translation",
			host: kdexv1alpha1.KDexHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.KDexHostSpec{
					DefaultLang:  "en",
					ModulePolicy: kdexv1alpha1.LooseModulePolicy,
					Organization: "KDex Tech Inc.",
					Routing: kdexv1alpha1.Routing{
						Domains: []string{"foo.bar"},
					},
				},
			},
			page: kdexv1alpha1.KDexPageBinding{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-render-page",
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					// PageComponents: kdexv1alpha1.PageComponents{
					// 	Contents: map[string]string{
					// 		"main": "MAIN",
					// 	},
					// 	Footer: "FOOTER",
					// 	Header: `{{ l10n "key" }}`,
					// 	Navigations: map[string]string{
					// 		"main": "NAV",
					// 	},
					// 	PrimaryTemplate: primaryTemplate,
					// 	Title:           "TITLE",
					// },
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

			th := NewHostHandler(logr.Discard())
			th.SetHost(&tt.host, nil, nil)
			th.AddOrUpdateTranslation(tt.translation)
			got, gotErr := th.L10nRenderLocked(PageHandler{
				Page: &tt.page,
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
		name        string
		host        kdexv1alpha1.KDexHost
		translation *kdexv1alpha1.KDexTranslation
		page        kdexv1alpha1.KDexPageBinding
		want        map[string][]string
	}{
		{
			name: "translations",
			host: kdexv1alpha1.KDexHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.KDexHostSpec{
					DefaultLang:  "en",
					ModulePolicy: kdexv1alpha1.LooseModulePolicy,
					Organization: "KDex Tech Inc.",
					Routing: kdexv1alpha1.Routing{
						Domains: []string{"foo.bar"},
					},
				},
			},
			page: kdexv1alpha1.KDexPageBinding{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-render-page",
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					// PageComponents: kdexv1alpha1.PageComponents{
					// 	Contents: map[string]string{
					// 		"main": "MAIN",
					// 	},
					// 	Footer: "FOOTER",
					// 	Header: `{{ l10n "key" }}`,
					// 	Navigations: map[string]string{
					// 		"main": "NAV",
					// 	},
					// 	PrimaryTemplate: primaryTemplate,
					// 	Title:           "TITLE",
					// },
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			},
			translation: &kdexv1alpha1.KDexTranslation{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-translation",
					Namespace: "foo",
				},
				Spec: kdexv1alpha1.KDexTranslationSpec{
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
					},
				},
			},
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

			th := NewHostHandler(logr.Discard())
			th.SetHost(&tt.host, nil, nil)
			th.AddOrUpdateTranslation(tt.translation)
			got := th.L10nRendersLocked(PageHandler{
				Page: &tt.page,
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
		host        kdexv1alpha1.KDexHost
		translation *kdexv1alpha1.KDexTranslation
		langTests   map[string]KeyAndExpected
	}{
		{
			name: "add translation",
			host: kdexv1alpha1.KDexHost{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-host",
				},
				Spec: kdexv1alpha1.KDexHostSpec{
					DefaultLang:  "en",
					ModulePolicy: kdexv1alpha1.LooseModulePolicy,
					Organization: "KDex Tech Inc.",
					Routing: kdexv1alpha1.Routing{
						Domains: []string{"foo.bar"},
					},
				},
			},
			translation: &kdexv1alpha1.KDexTranslation{
				ObjectMeta: v1.ObjectMeta{
					Name: "sample-translation",
				},
				Spec: kdexv1alpha1.KDexTranslationSpec{
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

			th := NewHostHandler(logr.Discard())
			th.SetHost(&tt.host, nil, nil)
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
