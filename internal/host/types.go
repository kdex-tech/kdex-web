package host

type PathType string

const (
	BackendPathType  PathType = "BACKEND"
	FunctionPathType PathType = "FUNCTION"
	InternalPathType PathType = "INTERNAL"
	PagePathType     PathType = "PAGE"
)

type PathInfo struct {
	Secondaries []string
	Type        PathType
}
