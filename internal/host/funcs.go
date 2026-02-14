package host

import (
	"maps"
	"net/url"
	"strings"

	openapi "github.com/getkin/kin-openapi/openapi3"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
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
