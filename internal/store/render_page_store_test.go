package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
)

func Test_RenderPageStore_BuildMenuEntries(t *testing.T) {
	tests := []struct {
		name              string
		items             *map[string]RenderPageHandler
		isDefaultLanguage bool
		want              *map[string]*render.PageEntry
	}{
		{
			name:  "empty",
			items: &map[string]RenderPageHandler{},
			want:  nil,
		},
		{
			name:              "simple",
			isDefaultLanguage: true,
			items: &map[string]RenderPageHandler{
				"foo": {
					Page: kdexv1alpha1.MicroFrontEndRenderPage{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/foo",
							},
							PageComponents: kdexv1alpha1.PageComponents{
								Title: "Foo",
							},
						},
					},
				},
			},
			want: &map[string]*render.PageEntry{
				"Foo Translated": {
					Name:   "foo",
					Label:  "Foo Translated",
					Href:   "/foo",
					Weight: resource.MustParse("0"),
				},
			},
		},
		{
			name:              "more complex",
			isDefaultLanguage: true,
			items: &map[string]RenderPageHandler{
				"foo": {
					Page: kdexv1alpha1.MicroFrontEndRenderPage{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/foo",
							},
							PageComponents: kdexv1alpha1.PageComponents{
								Title: "Foo",
							},
							ParentPageRef: &corev1.LocalObjectReference{
								Name: "home",
							},
						},
					},
				},
				"home": {
					Page: kdexv1alpha1.MicroFrontEndRenderPage{
						ObjectMeta: metav1.ObjectMeta{
							Name: "home",
						},
						Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/home",
							},
							PageComponents: kdexv1alpha1.PageComponents{
								Title: "Home",
							},
						},
					},
				},
				"contact": {
					Page: kdexv1alpha1.MicroFrontEndRenderPage{
						ObjectMeta: metav1.ObjectMeta{
							Name: "contact",
						},
						Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
							NavigationHints: &kdexv1alpha1.NavigationHints{
								Weight: resource.MustParse("100"),
							},
							Paths: kdexv1alpha1.Paths{
								BasePath: "/contact",
							},
							PageComponents: kdexv1alpha1.PageComponents{
								Title: "Contact Us",
							},
						},
					},
				},
			},
			want: &map[string]*render.PageEntry{
				"Home": {
					Children: &map[string]*render.PageEntry{
						"Foo Translated": {
							Href:   "/foo",
							Label:  "Foo Translated",
							Name:   "foo",
							Weight: resource.MustParse("0"),
						},
					},
					Href:   "/home",
					Label:  "Home",
					Name:   "home",
					Weight: resource.MustParse("0"),
				},
				"Contact Us": {
					Href:   "/contact",
					Label:  "Contact Us",
					Name:   "contact",
					Weight: resource.MustParse("100"),
				},
			},
		},
		{
			name:              "none default language",
			isDefaultLanguage: false,
			items: &map[string]RenderPageHandler{
				"foo": {
					Page: kdexv1alpha1.MicroFrontEndRenderPage{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/foo",
							},
							PageComponents: kdexv1alpha1.PageComponents{
								Title: "Foo",
							},
						},
					},
				},
			},
			want: &map[string]*render.PageEntry{
				"Foo Translated": {
					Name:   "foo",
					Label:  "Foo Translated",
					Href:   "/en/foo",
					Weight: resource.MustParse("0"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rps := RenderPageStore{
				handlers: *tt.items,
			}
			tag := language.English
			catalogBuilder := catalog.NewBuilder()
			catalogBuilder.SetString(language.English, "Foo", "Foo Translated")
			messagePrinter := message.NewPrinter(tag, message.Catalog(catalogBuilder))
			got := &render.PageEntry{}

			rps.BuildMenuEntries(got, &tag, messagePrinter, tt.isDefaultLanguage, nil)
			assert.Equal(t, tt.want, got.Children)
		})
	}
}
