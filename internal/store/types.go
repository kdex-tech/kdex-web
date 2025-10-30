package store

import (
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type RenderPageHandler struct {
	Page       kdexv1alpha1.KDexRenderPage
	Stylesheet *kdexv1alpha1.KDexStylesheet
}
