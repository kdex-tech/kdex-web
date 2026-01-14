package openapi

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	openapi "github.com/getkin/kin-openapi/openapi3"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

const (
	BackendPathType  PathType = "BACKEND"
	FunctionPathType PathType = "FUNCTION"
	InternalPathType PathType = "INTERNAL"
	PagePathType     PathType = "PAGE"
)

var wildcardRegex = regexp.MustCompile(`\.\.\.\}`)

type PathType string

type PathInfo struct {
	API         kdexv1alpha1.KDexOpenAPI
	Secondaries []string
	Type        PathType
}

func BuildOpenAPI(name string, paths map[string]PathInfo, filterPaths []string) *openapi.T {
	doc := &openapi.T{
		Components: &openapi.Components{
			Schemas:         openapi.Schemas{},
			SecuritySchemes: openapi.SecuritySchemes{},
		},
		Info: &openapi.Info{
			Title:       fmt.Sprintf("KDex Host: %s", name),
			Description: "Auto-generated OpenAPI specification for KDex Host",
			Version:     "1.0.0",
		},
		Paths:   &openapi.Paths{},
		OpenAPI: "3.0.0",
	}

	for _, info := range paths {
		path := toOpenAPIPath(info.API.Path)
		if path == "" {
			continue
		}

		if !slices.Contains(filterPaths, path) {
			continue
		}

		pathItem := &openapi.PathItem{
			Summary:     info.API.Summary,
			Description: info.API.Description,
		}

		// Fill path item operations
		ensureOpMetadata := func(op *openapi.Operation) {
			if op == nil {
				return
			}
			if op.Summary == "" {
				op.Summary = info.API.Summary
			}
			if op.Description == "" {
				op.Description = info.API.Description
			}
		}

		if info.API.Get != nil {
			ensureOpMetadata(info.API.Get)
			pathItem.Get = info.API.Get
		}
		if info.API.Put != nil {
			ensureOpMetadata(info.API.Put)
			pathItem.Put = info.API.Put
		}
		if info.API.Post != nil {
			ensureOpMetadata(info.API.Post)
			pathItem.Post = info.API.Post
		}
		if info.API.Delete != nil {
			ensureOpMetadata(info.API.Delete)
			pathItem.Delete = info.API.Delete
		}
		if info.API.Options != nil {
			ensureOpMetadata(info.API.Options)
			pathItem.Options = info.API.Options
		}
		if info.API.Head != nil {
			ensureOpMetadata(info.API.Head)
			pathItem.Head = info.API.Head
		}
		if info.API.Patch != nil {
			ensureOpMetadata(info.API.Patch)
			pathItem.Patch = info.API.Patch
		}
		if info.API.Trace != nil {
			ensureOpMetadata(info.API.Trace)
			pathItem.Trace = info.API.Trace
		}
		if info.API.Connect != nil {
			ensureOpMetadata(info.API.Connect)
			pathItem.Connect = info.API.Connect
		}

		doc.Paths.Set(path, pathItem)

		for key, schema := range info.API.Schemas {
			if !strings.HasPrefix(key, "#/components/schemas/") {
				key = "#/components/schemas/" + key
			}

			_, found := doc.Components.Schemas[key]
			if found {
				key = key + "_conflict_" + GenerateNameFromPath(path, "")
			}

			doc.Components.Schemas[key] = &openapi.SchemaRef{
				Value: &schema,
			}
		}
	}

	return doc
}

func ExtractParameters(path string, query string, header http.Header) openapi.Parameters {
	var params openapi.Parameters

	// Regular expression to match path parameters: {name} or {name...}
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(path, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		paramName := match[1]
		isWildcard := strings.HasSuffix(paramName, "...")

		// Clean the parameter name
		cleanName := strings.TrimSuffix(paramName, "...")

		// Create parameter description based on name
		description := fmt.Sprintf("Path parameter: %s", cleanName)
		if isWildcard {
			description = fmt.Sprintf("Wildcard path parameter: %s (captures remaining path segments)", cleanName)
		}

		// Determine schema type
		schema := openapi.NewStringSchema()
		if isWildcard {
			// Wildcard parameters can contain slashes and multiple segments
			schema.Description = "May contain multiple path segments separated by slashes"
		}

		param := &openapi.Parameter{
			Description: description,
			In:          "path",
			Name:        cleanName,
			Required:    true,
			Schema:      openapi.NewSchemaRef("", schema),
		}

		if isWildcard {
			param.AllowReserved = true
		}

		params = append(params, &openapi.ParameterRef{
			Value: param,
		})
	}

	// Parse the query string for parameters
	if query != "" {
		// Track parameter occurrences to detect arrays
		paramCounts := make(map[string]int)

		// Parse query string manually to count occurrences
		pairs := strings.Split(query, "&")
		for _, pair := range pairs {
			if pair == "" {
				continue
			}

			parts := strings.SplitN(pair, "=", 2)
			if len(parts) > 0 {
				key := parts[0]
				paramCounts[key]++
			}
		}

		// Create parameters for unique keys
		for paramName, count := range paramCounts {
			isArray := count > 1

			description := fmt.Sprintf("Query parameter: %s", paramName)
			if isArray {
				description = fmt.Sprintf("Query parameter: %s (array - multiple values supported)", paramName)
			}

			// Determine schema type
			var schema *openapi.Schema
			if isArray {
				schema = openapi.NewArraySchema()
				schema.Items = openapi.NewSchemaRef("", openapi.NewStringSchema())
			} else {
				schema = openapi.NewStringSchema()
			}

			param := &openapi.Parameter{
				Name:        paramName,
				In:          "query",
				Description: description,
				Required:    false, // Query parameters are typically optional
				Schema:      openapi.NewSchemaRef("", schema),
			}

			if isArray {
				param.Explode = openapi.Ptr(true)
			}

			params = append(params, &openapi.ParameterRef{
				Value: param,
			})
		}
	}

	// Parse header parameters (Selective)
	skipHeaders := map[string]bool{
		"accept":                    true,
		"accept-encoding":           true,
		"accept-language":           true,
		"authorization":             true,
		"connection":                true,
		"content-length":            true,
		"content-type":              true,
		"cookie":                    true,
		"expect":                    true,
		"host":                      true,
		"if-match":                  true,
		"if-none-match":             true,
		"if-modified-since":         true,
		"if-unmodified-since":       true,
		"origin":                    true,
		"priority":                  true,
		"referer":                   true,
		"sec-ch-ua":                 true,
		"sec-ch-ua-mobile":          true,
		"sec-ch-ua-platform":        true,
		"sec-fetch-dest":            true,
		"sec-fetch-mode":            true,
		"sec-fetch-site":            true,
		"sec-fetch-user":            true,
		"upgrade-insecure-requests": true,
		"user-agent":                true,
		"x-forwarded-for":           true,
		"x-forwarded-host":          true,
		"x-forwarded-proto":         true,
		"x-real-ip":                 true,
	}

	for name := range header {
		lowerName := strings.ToLower(name)
		if strings.HasPrefix(lowerName, "x-kdex-") || skipHeaders[lowerName] {
			continue
		}
		schema := openapi.NewArraySchema()
		schema.Items = openapi.NewSchemaRef("", openapi.NewStringSchema())

		param := &openapi.Parameter{
			Name: name,
			In:   "header",
			Schema: &openapi.SchemaRef{
				Value: schema,
			},
		}
		params = append(params, &openapi.ParameterRef{
			Value: param,
		})
	}

	return params
}

func GenerateNameFromPath(path string, headerName string) string {
	if headerName != "" {
		return headerName
	}

	cleanPath := strings.NewReplacer("{", "", "}", "", ".", "-", "$", "", " ", "-", "[", "", "]", "").Replace(path)
	name := strings.ToLower(cleanPath)
	name = strings.ReplaceAll(name, "/", "-")

	// Collapse multiple dashes and trim
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-'
	})
	name = strings.Join(parts, "-")

	if name == "" {
		return "gen-root"
	}
	return "gen-" + name
}

func MergeOperations(dest, src *kdexv1alpha1.KDexOpenAPI) {
	if src.Connect != nil {
		dest.Connect = src.Connect
	}
	if src.Delete != nil {
		dest.Delete = src.Delete
	}
	if src.Get != nil {
		dest.Get = src.Get
	}
	if src.Head != nil {
		dest.Head = src.Head
	}
	if src.Options != nil {
		dest.Options = src.Options
	}
	if src.Patch != nil {
		dest.Patch = src.Patch
	}
	if src.Post != nil {
		dest.Post = src.Post
	}
	if src.Put != nil {
		dest.Put = src.Put
	}
	if src.Trace != nil {
		dest.Trace = src.Trace
	}
}

func SetOperation(api *kdexv1alpha1.KDexOpenAPI, method string, op *openapi.Operation) {
	if op == nil {
		op = &openapi.Operation{}
	}

	// Extract and set parameters from the path if not already set
	if op.Parameters == nil && api.Path != "" {
		op.Parameters = ExtractParameters(api.Path, "", http.Header{})
	}

	switch strings.ToUpper(method) {
	case "CONNECT":
		api.Connect = op
	case "DELETE":
		api.Delete = op
	case "GET":
		api.Get = op
	case "HEAD":
		api.Head = op
	case "OPTIONS":
		api.Options = op
	case "PATCH":
		api.Patch = op
	case "POST":
		api.Post = op
	case "PUT":
		api.Put = op
	case "TRACE":
		api.Trace = op
	}
}

func toOpenAPIPath(path string) string {
	return wildcardRegex.ReplaceAllString(path, "}")
}
