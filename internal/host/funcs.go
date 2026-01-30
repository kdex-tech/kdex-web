package host

import (
	"net/url"
	"strings"

	ko "kdex.dev/web/internal/openapi"
)

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
