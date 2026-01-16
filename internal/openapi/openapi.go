package openapi

import (
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sort"
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

type Filter struct {
	Paths []string
	Tags  []string
	Type  PathType
}

type OpenAPI struct {
	Connect     *openapi.Operation
	Delete      *openapi.Operation
	Description string
	Get         *openapi.Operation
	Head        *openapi.Operation
	Options     *openapi.Operation
	Parameters  []openapi.Parameter
	Path        string
	Patch       *openapi.Operation
	Post        *openapi.Operation
	Put         *openapi.Operation
	Schemas     map[string]openapi.Schema
	Summary     string
	Trace       *openapi.Operation
}

func (o *OpenAPI) FromKDexAPI(k *kdexv1alpha1.KDexOpenAPI) *OpenAPI {
	return &OpenAPI{
		Connect:     k.GetConnect(),
		Delete:      k.GetDelete(),
		Description: k.Description,
		Get:         k.GetGet(),
		Head:        k.GetHead(),
		Options:     k.GetOptions(),
		Parameters:  k.GetParameters(),
		Path:        k.Path,
		Patch:       k.GetPatch(),
		Post:        k.GetPost(),
		Put:         k.GetPut(),
		Schemas:     k.GetSchemas(),
		Summary:     k.Summary,
		Trace:       k.GetTrace(),
	}
}

func (o *OpenAPI) SetOperation(method string, op *openapi.Operation) {
	switch method {
	case "CONNECT":
		o.Connect = op
	case "DELETE":
		o.Delete = op
	case "GET":
		o.Get = op
	case "HEAD":
		o.Head = op
	case "OPTIONS":
		o.Options = op
	case "PATCH":
		o.Patch = op
	case "POST":
		o.Post = op
	case "PUT":
		o.Put = op
	case "TRACE":
		o.Trace = op
	}
}

func (o *OpenAPI) ToKDexAPI() *kdexv1alpha1.KDexOpenAPI {
	k := &kdexv1alpha1.KDexOpenAPI{
		Description:         o.Description,
		KDexOpenAPIInternal: kdexv1alpha1.KDexOpenAPIInternal{},
		Path:                o.Path,
		Summary:             o.Summary,
	}

	k.SetConnect(o.Connect)
	k.SetDelete(o.Delete)
	k.SetGet(o.Get)
	k.SetHead(o.Head)
	k.SetOptions(o.Options)
	k.SetParameters(o.Parameters)
	k.SetPatch(o.Patch)
	k.SetPost(o.Post)
	k.SetPut(o.Put)
	k.SetSchemas(o.Schemas)
	k.SetTrace(o.Trace)

	return k
}

type PathType string

type PathInfo struct {
	API      OpenAPI
	Metadata *kdexv1alpha1.Metadata
	Type     PathType
}

func BuildOpenAPI(name string, paths map[string]PathInfo, filter Filter) *openapi.T {
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

	keys := slices.Collect(maps.Keys(paths))

	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	for _, key := range keys {
		pathInfo := paths[key]

		path := toOpenAPIPath(pathInfo.API.Path)
		if path == "" {
			continue
		}

		if !matchFilter(path, pathInfo, filter) {
			continue
		}

		pathItem := &openapi.PathItem{
			Description: pathInfo.API.Description,
			Summary:     pathInfo.API.Summary,
			Extensions: map[string]any{
				"x-kdex-type": pathInfo.Type,
			},
		}

		if pathInfo.Metadata != nil {
			pathItem.Extensions["x-kdex-metadata"] = pathInfo.Metadata
		}

		// Fill path item operations
		ensureOpMetadata := func(op *openapi.Operation) {
			if op == nil {
				return
			}
			if op.Summary == "" {
				op.Summary = pathInfo.API.Summary
			}
			if op.Description == "" {
				op.Description = pathInfo.API.Description
			}
		}

		if pathInfo.API.Get != nil {
			ensureOpMetadata(pathInfo.API.Get)
			pathItem.Get = pathInfo.API.Get
		}
		if pathInfo.API.Put != nil {
			ensureOpMetadata(pathInfo.API.Put)
			pathItem.Put = pathInfo.API.Put
		}
		if pathInfo.API.Post != nil {
			ensureOpMetadata(pathInfo.API.Post)
			pathItem.Post = pathInfo.API.Post
		}
		if pathInfo.API.Delete != nil {
			ensureOpMetadata(pathInfo.API.Delete)
			pathItem.Delete = pathInfo.API.Delete
		}
		if pathInfo.API.Options != nil {
			ensureOpMetadata(pathInfo.API.Options)
			pathItem.Options = pathInfo.API.Options
		}
		if pathInfo.API.Head != nil {
			ensureOpMetadata(pathInfo.API.Head)
			pathItem.Head = pathInfo.API.Head
		}
		if pathInfo.API.Patch != nil {
			ensureOpMetadata(pathInfo.API.Patch)
			pathItem.Patch = pathInfo.API.Patch
		}
		if pathInfo.API.Trace != nil {
			ensureOpMetadata(pathInfo.API.Trace)
			pathItem.Trace = pathInfo.API.Trace
		}
		if pathInfo.API.Connect != nil {
			ensureOpMetadata(pathInfo.API.Connect)
			pathItem.Connect = pathInfo.API.Connect
		}

		doc.Paths.Set(path, pathItem)

		for key, schema := range pathInfo.API.Schemas {
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
	path = strings.ReplaceAll(path, "{$}", "")

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
		"x-forwarded-port":          true,
		"x-forwarded-proto":         true,
		"x-forwarded-server":        true,
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

func GenerateNameFromPath(path string, headerValue string) string {
	if headerValue != "" {
		return headerValue
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

func GenerateOperationID(name string, method string, headerValue string) string {
	if headerValue != "" {
		return headerValue
	}
	return fmt.Sprintf("%s-%s", name, strings.ToLower(method))
}

func InferSchema(val any) *openapi.SchemaRef {
	schema := openapi.NewSchema()

	switch v := val.(type) {
	case string:
		schema.Type = &openapi.Types{openapi.TypeString}
	case bool:
		schema.Type = &openapi.Types{openapi.TypeBoolean}
	case float64:
		// JSON unmarshals all numbers as float64 by default
		schema.Type = &openapi.Types{openapi.TypeNumber}
	case map[string]any:
		schema.Type = &openapi.Types{openapi.TypeObject}
		schema.Properties = make(openapi.Schemas)
		for key, subVal := range v {
			schema.Properties[key] = InferSchema(subVal)
		}
	case []any:
		schema.Type = &openapi.Types{openapi.TypeArray}
		if len(v) > 0 {
			// Infer item type from the first element
			schema.Items = InferSchema(v[0])
		}
	case nil:
		schema.Nullable = true
	}

	return schema.NewRef()
}

func MergeOperations(dest, src *OpenAPI) {
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

func matchFilter(path string, info PathInfo, filter Filter) bool {
	if len(filter.Paths) > 0 && !slices.Contains(filter.Paths, path) {
		return false
	}

	if len(filter.Tags) > 0 {
		for _, tag := range filter.Tags {
			if !slices.Contains(info.Metadata.Tags, tag) {
				return false
			}
		}
	}

	if filter.Type != "" && info.Type != filter.Type {
		return false
	}

	return true
}

func toOpenAPIPath(path string) string {
	return strings.ReplaceAll(wildcardRegex.ReplaceAllString(path, "}"), "{$}", "")
}
