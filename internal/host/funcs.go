package host

import (
	"net/url"
	"strings"

	openapi "github.com/getkin/kin-openapi/openapi3"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ko "kdex.dev/web/internal/openapi"
)

func (hh *HostHandler) convertRequirements(in *[]kdexv1alpha1.SecurityRequirement) (*openapi.SecurityRequirements, map[string]interface{}) {
	var out *openapi.SecurityRequirements
	var extensions map[string]any
	requiredClaims := make(map[string][]string)

	if in == nil {
		return out, nil
	}

	out = &openapi.SecurityRequirements{}

	for _, rIn := range *in {
		rNew := openapi.SecurityRequirement{}
		for name, scopes := range rIn {
			if name == "bearer" {
				rNew[name] = []string{}
				if len(scopes) > 0 {
					requiredClaims[name] = append(requiredClaims[name], scopes...)
				}
			} else {
				rNew[name] = scopes
			}
		}
		out.With(rNew)
	}

	if len(requiredClaims) > 0 {
		extensions = map[string]any{
			"x-required-jwt-claims": requiredClaims,
		}
	}

	return out, extensions
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
