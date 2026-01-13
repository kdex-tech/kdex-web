package host

import (
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

type PathType string

const (
	BackendPathType  PathType = "BACKEND"
	FunctionPathType PathType = "FUNCTION"
	InternalPathType PathType = "INTERNAL"
	PagePathType     PathType = "PAGE"
)

type PathInfo struct {
	API         kdexv1alpha1.KDexOpenAPI
	Secondaries []string
	Type        PathType
}
