package host

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"

	openapi "github.com/getkin/kin-openapi/openapi3"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	kh "kdex.dev/web/internal/http"
	ko "kdex.dev/web/internal/openapi"
)

func (hh *HostHandler) convertRequirements(in *[]kdexv1alpha1.SecurityRequirement) *openapi.SecurityRequirements {
	var out *openapi.SecurityRequirements

	if in == nil {
		return out
	}

	out = &openapi.SecurityRequirements{}

	for _, rIn := range *in {
		rNew := openapi.SecurityRequirement{}
		maps.Copy(rNew, rIn)
		out.With(rNew)
	}

	return out
}

func (hh *HostHandler) handleAuth(
	r *http.Request,
	w http.ResponseWriter,
	resource string,
	resourceName string,
	requirements []kdexv1alpha1.SecurityRequirement,
) bool {
	if !hh.authConfig.IsAuthEnabled() {
		return false
	}

	authorized, err := hh.authChecker.CheckAccess(
		r.Context(), resource, resourceName, requirements)

	if err != nil {
		hh.log.Error(err, "authorization check failed", resource, resourceName)
		r.Header.Set("X-KDex-Sniffer-Skip", "true")
		http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
		return true
	}

	if !authorized {
		r.Header.Set("X-KDex-Sniffer-Skip", "true")
		hh.log.V(1).Info("unauthorized access attempt", resource, resourceName)
		http.Error(w, http.StatusText(http.StatusNotFound)+" "+r.URL.Path, http.StatusNotFound)
		return true
	}

	return false
}

// Helper to strip the Domain attribute from a Set-Cookie string
func (hh *HostHandler) stripCookieDomain(cookieStr string) string {
	parts := strings.Split(cookieStr, ";")
	var newParts []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if !strings.HasPrefix(strings.ToLower(trimmed), "domain=") {
			newParts = append(newParts, part)
		}
	}
	return strings.Join(newParts, ";")
}

func filterFromQuery(queryParams url.Values) ko.Filter {
	filter := ko.Filter{}

	pathParams := queryParams["path"]
	if len(pathParams) > 0 {
		filter.Paths = pathParams
	}

	tagParams := queryParams["tag"]
	if len(tagParams) > 0 {
		filter.Tags = tagParams
	}

	typeParams := queryParams["type"]
	if len(typeParams) > 0 {
		for _, t := range typeParams {
			switch strings.ToUpper(t) {
			case string(ko.BackendPathType):
				filter.Type = append(filter.Type, ko.BackendPathType)
			case string(ko.FunctionPathType):
				filter.Type = append(filter.Type, ko.FunctionPathType)
			case string(ko.PagePathType):
				filter.Type = append(filter.Type, ko.PagePathType)
			case string(ko.SystemPathType):
				filter.Type = append(filter.Type, ko.SystemPathType)
			}
		}
	}

	return filter
}

func functionCallRequirements(
	r *http.Request, fn *kdexv1alpha1.KDexFunction,
) []kdexv1alpha1.SecurityRequirement {
	var requirements []kdexv1alpha1.SecurityRequirement

	routes := []string{}
	for path, pathItem := range fn.Spec.API.Paths {
		if pathItem.Connect != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodConnect, path))
		}
		if pathItem.Delete != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodDelete, path))
		}
		if pathItem.Get != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodGet, path))
		}
		if pathItem.Head != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodHead, path))
		}
		if pathItem.Options != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodOptions, path))
		}
		if pathItem.Patch != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodPatch, path))
		}
		if pathItem.Post != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodPost, path))
		}
		if pathItem.Put != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodPut, path))
		}
		if pathItem.Trace != nil {
			routes = append(routes, fmt.Sprintf("%s %s", http.MethodTrace, path))
		}
	}

	pattern, _ := kh.DiscoverPattern(routes, r)
	if pattern != "" {
		parts := strings.Split(pattern, " ")
		pathItem := fn.Spec.API.Paths[parts[1]]
		op := pathItem.GetOp(parts[0])
		if op != nil && op.Security != nil {
			for _, s := range *op.Security {
				requirements = append(requirements, kdexv1alpha1.SecurityRequirement(s))
			}
		}
	}

	return requirements
}
