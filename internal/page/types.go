package page

import (
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type PageHandler struct {
	// root object
	Page *kdexv1alpha1.KDexPageBinding

	// dereferenced resources
	Content           map[string]PackedContent
	Footer            string
	Header            string
	MainTemplate      string
	Navigations       map[string]string
	PackageReferences []kdexv1alpha1.PackageReference
	Scripts           []kdexv1alpha1.ScriptDef
}

type PackedContent struct {
	AppName           string
	AppGeneration     string
	Content           string
	CustomElementName string
	Slot              string
}

type ResolvedContentEntry struct {
	App     *kdexv1alpha1.KDexAppSpec
	Content PackedContent
}

type ResolvedNavigation struct {
	Generation int64
	Name       string
	Spec       *kdexv1alpha1.KDexPageNavigationSpec
}
