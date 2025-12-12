package page

import (
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type PageHandler struct {
	// root object
	Page *kdexv1alpha1.KDexPageBinding

	// dereferenced resources
	Archetype         *kdexv1alpha1.KDexPageArchetypeSpec
	Content           map[string]ResolvedContentEntry
	Footer            *kdexv1alpha1.KDexPageFooterSpec
	Header            *kdexv1alpha1.KDexPageHeaderSpec
	Navigations       map[string]ResolvedNavigation
	PackageReferences []kdexv1alpha1.PackageReference
	ScriptLibraries   []kdexv1alpha1.KDexScriptLibrarySpec
}

type ResolvedContentEntry struct {
	App               *kdexv1alpha1.KDexAppSpec
	AppName           string
	AppGeneration     string
	Content           string
	CustomElementName string
	Slot              string
}

type ResolvedNavigation struct {
	Generation int64
	Name       string
	Spec       *kdexv1alpha1.KDexPageNavigationSpec
}
