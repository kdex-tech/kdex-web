package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/render"
)

func Test_PageStore_BuildMenuEntries(t *testing.T) {
	tests := []struct {
		name              string
		items             *map[string]PageHandler
		isDefaultLanguage bool
		want              *map[string]*render.PageEntry
	}{
		{
			name:  "empty",
			items: &map[string]PageHandler{},
			want:  nil,
		},
		{
			name:              "simple",
			isDefaultLanguage: true,
			items: &map[string]PageHandler{
				"foo": {
					Page: &kdexv1alpha1.KDexPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.KDexPageBindingSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/foo",
							},
							// PageComponents: kdexv1alpha1.PageComponents{
							// 	Title: "Foo",
							// },
						},
					},
				},
			},
			want: &map[string]*render.PageEntry{
				"Foo": {
					Href:   "/en/foo",
					Label:  "Foo",
					Name:   "foo",
					Weight: resource.MustParse("0"),
				},
			},
		},
		{
			name:              "more complex",
			isDefaultLanguage: true,
			items: &map[string]PageHandler{
				"foo": {
					Page: &kdexv1alpha1.KDexPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.KDexPageBindingSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/foo",
							},
							// PageComponents: kdexv1alpha1.PageComponents{
							// 	Title: "Foo",
							// },
							ParentPageRef: &corev1.LocalObjectReference{
								Name: "home",
							},
						},
					},
				},
				"home": {
					Page: &kdexv1alpha1.KDexPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "home",
						},
						Spec: kdexv1alpha1.KDexPageBindingSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/home",
							},
							// PageComponents: kdexv1alpha1.PageComponents{
							// 	Title: "Home",
							// },
						},
					},
				},
				"contact": {
					Page: &kdexv1alpha1.KDexPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "contact",
						},
						Spec: kdexv1alpha1.KDexPageBindingSpec{
							NavigationHints: &kdexv1alpha1.NavigationHints{
								Weight: resource.MustParse("100"),
							},
							Paths: kdexv1alpha1.Paths{
								BasePath: "/contact",
							},
							// PageComponents: kdexv1alpha1.PageComponents{
							// 	Title: "Contact Us",
							// },
						},
					},
				},
			},
			want: &map[string]*render.PageEntry{
				"Home": {
					Children: &map[string]*render.PageEntry{
						"Foo": {
							Href:   "/en/foo",
							Label:  "Foo",
							Name:   "foo",
							Weight: resource.MustParse("0"),
						},
					},
					Href:   "/en/home",
					Label:  "Home",
					Name:   "home",
					Weight: resource.MustParse("0"),
				},
				"Contact Us": {
					Href:   "/en/contact",
					Label:  "Contact Us",
					Name:   "contact",
					Weight: resource.MustParse("100"),
				},
			},
		},
		{
			name:              "none default language",
			isDefaultLanguage: false,
			items: &map[string]PageHandler{
				"foo": {
					Page: &kdexv1alpha1.KDexPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.KDexPageBindingSpec{
							Paths: kdexv1alpha1.Paths{
								BasePath: "/foo",
							},
							// PageComponents: kdexv1alpha1.PageComponents{
							// 	Title: "Foo",
							// },
						},
					},
				},
			},
			want: &map[string]*render.PageEntry{
				"Foo": {
					Name:   "foo",
					Label:  "Foo",
					Href:   "/en/foo",
					Weight: resource.MustParse("0"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rps := PageStore{
				handlers: *tt.items,
			}
			tag := language.English
			catalogBuilder := catalog.NewBuilder()
			catalogBuilder.SetString(language.English, "Foo", "Foo Translated")
			got := &render.PageEntry{}

			rps.BuildMenuEntries(got, &tag, tt.isDefaultLanguage, nil)
			assert.Equal(t, tt.want, got.Children)
		})
	}
}
