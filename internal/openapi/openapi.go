package openapi

import (
	"crypto/rand"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
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
	BasePath string
	Paths    map[string]PathItem
	Schemas  map[string]*openapi.SchemaRef
}

type PathItem struct {
	Connect     *openapi.Operation
	Delete      *openapi.Operation
	Description string
	Get         *openapi.Operation
	Head        *openapi.Operation
	Options     *openapi.Operation
	Parameters  []openapi.Parameter
	Patch       *openapi.Operation
	Post        *openapi.Operation
	Put         *openapi.Operation
	Summary     string
	Trace       *openapi.Operation
}

func FromKDexAPI(k *kdexv1alpha1.API) *OpenAPI {
	openAPI := &OpenAPI{
		BasePath: k.BasePath,
		Paths:    map[string]PathItem{},
		Schemas:  k.GetSchemas(),
	}

	for curPath, curItem := range k.Paths {
		openAPI.Paths[curPath] = PathItem{
			Connect:     curItem.GetConnect(),
			Delete:      curItem.GetDelete(),
			Description: curItem.Description,
			Get:         curItem.GetGet(),
			Head:        curItem.GetHead(),
			Options:     curItem.GetOptions(),
			Parameters:  curItem.GetParameters(),
			Patch:       curItem.GetPatch(),
			Post:        curItem.GetPost(),
			Put:         curItem.GetPut(),
			Summary:     curItem.Summary,
			Trace:       curItem.GetTrace(),
		}
	}

	return openAPI
}

func (o *PathItem) SetOperation(method string, op *openapi.Operation) {
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

func (o *OpenAPI) ToKDexAPI() *kdexv1alpha1.API {
	k := &kdexv1alpha1.API{
		BasePath: o.BasePath,
		Paths:    map[string]kdexv1alpha1.PathItem{},
	}

	for curPath, curItem := range o.Paths {
		item := kdexv1alpha1.PathItem{
			Description: curItem.Description,
			Summary:     curItem.Summary,
		}

		item.SetConnect(curItem.Connect)
		item.SetDelete(curItem.Delete)
		item.SetGet(curItem.Get)
		item.SetHead(curItem.Head)
		item.SetOptions(curItem.Options)
		item.SetParameters(curItem.Parameters)
		item.SetPatch(curItem.Patch)
		item.SetPost(curItem.Post)
		item.SetPut(curItem.Put)
		item.SetTrace(curItem.Trace)

		k.Paths[curPath] = item
	}

	k.SetSchemas(o.Schemas)

	return k
}

type PathType string

type PathInfo struct {
	API      OpenAPI
	Metadata *kdexv1alpha1.Metadata
	Type     PathType
}

func BuildOneOff(serverUrl string, fn *kdexv1alpha1.KDexFunction) *openapi.T {
	api := FromKDexAPI(&fn.Spec.API)
	info := PathInfo{
		API:  *api,
		Type: FunctionPathType,
	}
	paths := map[string]PathInfo{
		fn.Spec.API.BasePath: info,
	}

	return BuildOpenAPI(serverUrl, fn.Name, paths, Filter{})
}

func BuildOpenAPI(serverUrl string, name string, paths map[string]PathInfo, filter Filter) *openapi.T {
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
		Servers: openapi.Servers{
			&openapi.Server{
				URL: serverUrl,
			},
		},
		Tags: openapi.Tags{},
	}

	basePaths := slices.Collect(maps.Keys(paths))

	sort.Slice(basePaths, func(i, j int) bool {
		return basePaths[i] < basePaths[j]
	})

	for _, basePath := range basePaths {
		pathInfo := paths[basePath]

		for curPatternPath, curItem := range pathInfo.API.Paths {
			openapiPath := toOpenAPIPath(curPatternPath)
			if openapiPath == "" {
				continue
			}

			if !matchFilter(openapiPath, pathInfo, filter) {
				continue
			}

			doc.Tags = append(doc.Tags, collectTags(pathInfo)...)

			pathItem := &openapi.PathItem{
				Description: curItem.Description,
				Summary:     curItem.Summary,
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
					op.Summary = curItem.Summary
				}
				if op.Description == "" {
					op.Description = curItem.Description
				}
			}

			if curItem.Get != nil {
				ensureOpMetadata(curItem.Get)
				pathItem.Get = curItem.Get
			}
			if curItem.Put != nil {
				ensureOpMetadata(curItem.Put)
				pathItem.Put = curItem.Put
			}
			if curItem.Post != nil {
				ensureOpMetadata(curItem.Post)
				pathItem.Post = curItem.Post
			}
			if curItem.Delete != nil {
				ensureOpMetadata(curItem.Delete)
				pathItem.Delete = curItem.Delete
			}
			if curItem.Options != nil {
				ensureOpMetadata(curItem.Options)
				pathItem.Options = curItem.Options
			}
			if curItem.Head != nil {
				ensureOpMetadata(curItem.Head)
				pathItem.Head = curItem.Head
			}
			if curItem.Patch != nil {
				ensureOpMetadata(curItem.Patch)
				pathItem.Patch = curItem.Patch
			}
			if curItem.Trace != nil {
				ensureOpMetadata(curItem.Trace)
				pathItem.Trace = curItem.Trace
			}
			if curItem.Connect != nil {
				ensureOpMetadata(curItem.Connect)
				pathItem.Connect = curItem.Connect
			}

			doc.Paths.Set(openapiPath, pathItem)
		}

		for key, schema := range pathInfo.API.Schemas {
			_, found := doc.Components.Schemas[key]
			if found {
				key = key + ":conflict:" + rand.Text()[0:4]
			}

			doc.Components.Schemas[key] = schema
		}
	}

	return doc
}

func collectTags(pathInfo PathInfo) []*openapi.Tag {
	tags := []string{}

	for _, curItem := range pathInfo.API.Paths {
		if curItem.Connect != nil {
			tags = append(tags, curItem.Connect.Tags...)
		}
		if curItem.Delete != nil {
			tags = append(tags, curItem.Delete.Tags...)
		}
		if curItem.Get != nil {
			tags = append(tags, curItem.Get.Tags...)
		}
		if curItem.Head != nil {
			tags = append(tags, curItem.Head.Tags...)
		}
		if curItem.Options != nil {
			tags = append(tags, curItem.Options.Tags...)
		}
		if curItem.Patch != nil {
			tags = append(tags, curItem.Patch.Tags...)
		}
		if curItem.Post != nil {
			tags = append(tags, curItem.Post.Tags...)
		}
		if curItem.Put != nil {
			tags = append(tags, curItem.Put.Tags...)
		}
		if curItem.Trace != nil {
			tags = append(tags, curItem.Trace.Tags...)
		}
	}

	openApiTags := []*openapi.Tag{}

	seen := map[string]bool{}
	for _, tag := range tags {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		openApiTags = append(openApiTags, &openapi.Tag{
			Name: tag,
		})
	}

	return openApiTags
}

func ExtractParameters(routePath string, query string, header http.Header) openapi.Parameters {
	routePath = strings.ReplaceAll(routePath, "{$}", "")

	var params openapi.Parameters

	// Regular expression to match path parameters: {name} or {name...}
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(routePath, -1)

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

func GenerateNameFromPath(routePath string, headerValue string) string {
	if headerValue != "" {
		return headerValue
	}

	cleanPath := strings.NewReplacer("{", "", "}", "", ".", "-", "$", "", " ", "-", "[", "", "]", "").Replace(routePath)
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

func Host(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost" // Best effort
	}
	return fmt.Sprintf("%s://%s", scheme, host)
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
	for srcPath, srcItem := range src.Paths {
		destItem, ok := dest.Paths[srcPath]

		if !ok {
			dest.Paths[srcPath] = srcItem
		} else {
			if srcItem.Connect != nil {
				destItem.Connect = srcItem.Connect
			}
			if srcItem.Delete != nil {
				destItem.Delete = srcItem.Delete
			}
			if srcItem.Get != nil {
				destItem.Get = srcItem.Get
			}
			if srcItem.Head != nil {
				destItem.Head = srcItem.Head
			}
			if srcItem.Options != nil {
				destItem.Options = srcItem.Options
			}
			if srcItem.Patch != nil {
				destItem.Patch = srcItem.Patch
			}
			if srcItem.Post != nil {
				destItem.Post = srcItem.Post
			}
			if srcItem.Put != nil {
				destItem.Put = srcItem.Put
			}
			if srcItem.Trace != nil {
				destItem.Trace = srcItem.Trace
			}

			dest.Paths[srcPath] = destItem
		}
	}
}

func ExtractSchemaName(schemaString string) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(schemaString, "/"))

	if err != nil {
		return "", err
	}

	base := parsed.Path

	if strings.Contains(base, "/") {
		base = path.Base(base)
	}

	if base == "" {
		base = parsed.Host
	}

	if base == "" && parsed.Fragment != "" {
		base = parsed.Fragment
	}

	base = strings.TrimPrefix(base, "#")
	base = strings.TrimPrefix(base, "/components/schemas/")

	return base, nil
}

func matchFilter(routePath string, info PathInfo, filter Filter) bool {
	if len(filter.Paths) > 0 && !slices.Contains(filter.Paths, routePath) {
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

func toOpenAPIPath(routePath string) string {
	return strings.ReplaceAll(wildcardRegex.ReplaceAllString(routePath, "}"), "{$}", "")
}
