package page

import (
	"testing"

	"github.com/go-logr/logr"
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
		want              *map[string]interface{}
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
					Page: &kdexv1alpha1.KDexInternalPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.KDexInternalPageBindingSpec{
							KDexPageBindingSpec: kdexv1alpha1.KDexPageBindingSpec{
								Label: "Foo",
								Paths: kdexv1alpha1.Paths{
									BasePath: "/foo",
								},
							},
						},
					},
				},
			},
			want: &map[string]interface{}{
				"Foo": render.PageEntry{
					BasePath: "/foo",
					Href:     "/foo",
					Label:    "Foo",
					Name:     "foo",
					Weight:   resource.MustParse("0"),
				},
			},
		},
		{
			name:              "more complex",
			isDefaultLanguage: true,
			items: &map[string]PageHandler{
				"foo": {
					Page: &kdexv1alpha1.KDexInternalPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.KDexInternalPageBindingSpec{
							KDexPageBindingSpec: kdexv1alpha1.KDexPageBindingSpec{
								Label: "Foo",
								Paths: kdexv1alpha1.Paths{
									BasePath: "/foo",
								},
								ParentPageRef: &corev1.LocalObjectReference{
									Name: "home",
								},
							},
						},
					},
				},
				"home": {
					Page: &kdexv1alpha1.KDexInternalPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "home",
						},
						Spec: kdexv1alpha1.KDexInternalPageBindingSpec{
							KDexPageBindingSpec: kdexv1alpha1.KDexPageBindingSpec{
								Label: "Home",
								Paths: kdexv1alpha1.Paths{
									BasePath: "/home",
								},
							},
						},
					},
				},
				"contact": {
					Page: &kdexv1alpha1.KDexInternalPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "contact",
						},
						Spec: kdexv1alpha1.KDexInternalPageBindingSpec{
							KDexPageBindingSpec: kdexv1alpha1.KDexPageBindingSpec{
								Label: "Contact Us",
								NavigationHints: &kdexv1alpha1.NavigationHints{
									Weight: resource.MustParse("100"),
								},
								Paths: kdexv1alpha1.Paths{
									BasePath: "/contact",
								},
							},
						},
					},
				},
			},
			want: &map[string]interface{}{
				"Home": render.PageEntry{
					BasePath: "/home",
					Children: &map[string]interface{}{
						"Foo": render.PageEntry{
							BasePath: "/foo",
							Href:     "/foo",
							Label:    "Foo",
							Name:     "foo",
							Weight:   resource.MustParse("0"),
						},
					},
					Href:   "/home",
					Label:  "Home",
					Name:   "home",
					Weight: resource.MustParse("0"),
				},
				"Contact Us": render.PageEntry{
					BasePath: "/contact",
					Href:     "/contact",
					Label:    "Contact Us",
					Name:     "contact",
					Weight:   resource.MustParse("100"),
				},
			},
		},
		{
			name:              "none default language",
			isDefaultLanguage: false,
			items: &map[string]PageHandler{
				"foo": {
					Page: &kdexv1alpha1.KDexInternalPageBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.KDexInternalPageBindingSpec{
							KDexPageBindingSpec: kdexv1alpha1.KDexPageBindingSpec{
								Label: "Foo",
								Paths: kdexv1alpha1.Paths{
									BasePath: "/foo",
								},
							},
						},
					},
				},
			},
			want: &map[string]interface{}{
				"Foo": render.PageEntry{
					BasePath: "/foo",
					Href:     "/en/foo",
					Label:    "Foo",
					Name:     "foo",
					Weight:   resource.MustParse("0"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := NewPageStore("test", nil, logr.Discard())
			for _, it := range *tt.items {
				ps.Set(it)
			}
			tag := language.English
			catalogBuilder := catalog.NewBuilder()
			catalogBuilder.SetString(language.English, "Foo", "Foo Translated")
			got := &render.PageEntry{}

			ps.BuildMenuEntries(got, &tag, tt.isDefaultLanguage, nil)
			children := got.Children
			assert.Equal(t, tt.want, children)
		})
	}
}
