package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

func Test_RenderPageStore_BuildMenuEntries(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		items *map[string]RenderPageHandler
		want  *map[string]*kdexv1alpha1.PageEntry
	}{
		{
			name:  "empty",
			items: &map[string]RenderPageHandler{},
			want:  nil,
		},
		{
			name: "simple",
			items: &map[string]RenderPageHandler{
				"foo": {
					Page: kdexv1alpha1.MicroFrontEndRenderPage{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
							Path: "/foo",
							PageComponents: kdexv1alpha1.PageComponents{
								Title: "Foo",
							},
						},
					},
				},
			},
			want: &map[string]*kdexv1alpha1.PageEntry{
				"Foo": {
					Name:   "foo",
					Label:  "Foo",
					Href:   "/foo",
					Weight: resource.MustParse("0"),
				},
			},
		},
		{
			name: "more complex",
			items: &map[string]RenderPageHandler{
				"foo": {
					Page: kdexv1alpha1.MicroFrontEndRenderPage{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
						Spec: kdexv1alpha1.MicroFrontEndRenderPageSpec{
							Path: "/foo",
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
							Path: "/home",
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
							Path: "/contact",
							PageComponents: kdexv1alpha1.PageComponents{
								Title: "Contact Us",
							},
						},
					},
				},
			},
			want: &map[string]*kdexv1alpha1.PageEntry{
				"Home": {
					Children: &map[string]*kdexv1alpha1.PageEntry{
						"Foo": {
							Href:   "/foo",
							Label:  "Foo",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rps := RenderPageStore{
				handlers: *tt.items,
			}
			got := &kdexv1alpha1.PageEntry{}
			rps.BuildMenuEntries(got, nil)
			assert.Equal(t, tt.want, got.Children)
		})
	}
}
