package store

import (
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type PageHandler struct {
	// root object
	Page *kdexv1alpha1.KDexPageBinding

	// dereferenced resources
	Content         map[string]ResolvedContentEntry
	Footer          *kdexv1alpha1.KDexPageFooter
	Header          *kdexv1alpha1.KDexPageHeader
	Navigations     map[string]*kdexv1alpha1.KDexPageNavigation
	PageArchetype   *kdexv1alpha1.KDexPageArchetype
	ScriptLibraries []kdexv1alpha1.KDexScriptLibrary
}

type ResolvedContentEntry struct {
	Content string
	App     *kdexv1alpha1.KDexApp
}
