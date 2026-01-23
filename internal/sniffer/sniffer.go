package sniffer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	openapi "github.com/getkin/kin-openapi/openapi3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	kh "kdex.dev/web/internal/http"
	"kdex.dev/web/internal/linter"
	"kdex.dev/web/internal/mime"
	ko "kdex.dev/web/internal/openapi"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	docs = `
# KDex Request Sniffer Documentation

The KDex Request Sniffer automatically generates or updates KDexFunction resources by observing unhandled requests (404s).

## Supported Signals

### Custom HTTP Headers

- **X-KDex-Function-Comprehensive-Mode**: Set to "true" to enable Comprehensive API Modelling. This automatically generates a full suite of CRUD operations (GET, POST, PUT, PATCH, DELETE) for both collection and resource paths based on the pattern path.
- **X-KDex-Function-Deprecated**: Set to "true" to mark the operation as deprecated.
- **X-KDex-Function-Description**: Sets the OpenAPI operation description.
- **X-KDex-Function-Keep-Schema-Conflict**: Tells OpenAPI to keep conflicting under a special no-conflict key. Diagnostic feature.
- **X-KDex-Function-Name**: Specifies the name for the generated KDexFunction CR (standard Kubernetes naming rules apply).
- **X-KDex-Function-Operation-ID**: Sets a specific operationId in OpenAPI.
- **X-KDex-Function-Overwrite-Operation**: Set to "true" to overwrite an operation which is an exact match. Normally this would be rejected for satefy.
- **X-KDex-Function-Pattern-Path**: Specifies a net/http pattern path (e.g., "/users/{id}"). 
  - Must start with "/"
  - Must NOT contain a method (e.g. use "/users/{id}" not "GET /users/{id}")
  - Variables are supported: "{name}", "{path...}"
- **X-KDex-Function-Request-Schema-Ref**: Sets the OpenAPI operation request body schema reference. (e.g. "Foo", "#/components/schemas/Foo" or a URL to an external schema)
- **X-KDex-Function-Response-Schema-Ref**: Sets the OpenAPI operation response schema reference. (e.g. "Foo", "#/components/schemas/Foo" or a URL to an external schema)
- **X-KDex-Function-Summary**: Sets the OpenAPI operation summary.
- **X-KDex-Function-Tags**: Comma-separated list of tags for the OpenAPI operation.

### Core Header Introspection

- **Authorization**: If present, the sniffer signals that the route requires authentication. It adds security requirements matching the host's available modes (e.g., "bearer") and injects a "401 Unauthorized" response.
- **Accept**: If present and specific (not "*/*"), media types are used to populate the expected response "content" types in OpenAPI.
- **Content-Type**:
  - "application/json": The sniffer peeks at the body and infers a basic schema (types: string, number, boolean, object, array).
  - "application/x-www-form-urlencoded": The sniffer parses form fields and adds them as properties in the request body schema.

### Query Parameters

- Multi-value parameters (e.g., "?id=1&id=2") are detected and documented as "array" types in OpenAPI with "Explode: true".

---
*Note: The sniffer only processes non-internal paths (paths not starting with "/~") that result in a 404.*
`
	TRUE = "true"
)

var jsonMimeRegex = regexp.MustCompile(`^application\/(.*\+)?json(;.*)?$`)
var urlSchemeRegex regexp.Regexp = *regexp.MustCompile("^https?://.*")

type AnalysisResult struct {
	OriginalRequest *http.Request
	Function        *kdexv1alpha1.KDexFunction
	Lints           []string
}

type RequestSniffer struct {
	BasePathRegex  regexp.Regexp
	Client         client.Client
	Functions      []kdexv1alpha1.KDexFunction
	HostName       string
	ItemPathRegex  regexp.Regexp
	Namespace      string
	OpenAPIBuilder ko.Builder
	Security       *openapi.SecurityRequirements
}

func (s *RequestSniffer) Analyze(r *http.Request) (*AnalysisResult, error) {
	res, err := s.analyze(r)
	if err != nil {
		return nil, err
	}
	fnMutated := res.Function
	if fnMutated == nil {
		// Nil function means sniff returned no result (e.g. internal path)
		return nil, nil
	}

	fn := &kdexv1alpha1.KDexFunction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fnMutated.Name,
			Namespace: fnMutated.Namespace,
		},
		Status: kdexv1alpha1.KDexFunctionStatus{
			OpenAPISchemaURL: ko.Host(r) + "/~/openapi?tag=" + fnMutated.Name,
		},
	}

	op, err := ctrl.CreateOrUpdate(
		context.Background(), s.Client, fn,
		func() error {
			if fn.CreationTimestamp.IsZero() {
				fn.Annotations = make(map[string]string)
				fn.Labels = make(map[string]string)

				fn.Labels["app.kubernetes.io/name"] = "kdex-web"
				fn.Labels["kdex.dev/instance"] = s.HostName
			}

			fn.Spec = fnMutated.Spec

			if fn.CreationTimestamp.IsZero() {
				fn.Spec.HostRef = v1.LocalObjectReference{
					Name: s.HostName,
				}
			}

			return nil
		},
	)

	log := logf.FromContext(r.Context())

	log.V(2).Info(
		"sniffed function",
		"fnMutated", fnMutated,
		"op", op,
		"err", err,
	)

	if err == nil {
		fn.Status.OpenAPISchemaURL = ko.Host(r) + "/~/openapi?tag=" + fn.Name
		if statusErr := s.Client.Status().Update(context.Background(), fn); statusErr != nil {
			log.Error(statusErr, "failed to update function status", "name", fn.Name)
		}
	}

	return res, err
}

func (s *RequestSniffer) DocsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown")
	_, _ = w.Write([]byte(docs))
}

func (s *RequestSniffer) analyze(r *http.Request) (*AnalysisResult, error) {
	fn, err := s.sniff(r)
	if err != nil {
		return nil, err
	}

	res := &AnalysisResult{
		OriginalRequest: r,
		Function:        fn,
		Lints:           []string{},
	}

	if fn != nil {
		// Basic inference insights
		if r.Header.Get("Authorization") != "" {
			res.Lints = append(res.Lints, "[inference] Detected 'Authorization' header; secured endpoint inferred.")
		}
		if len(r.URL.Query()) > 0 {
			res.Lints = append(res.Lints, fmt.Sprintf("[inference] Detected %d query parameters.", len(r.URL.Query())))
		}

		// Run vacuum linter
		spec := s.OpenAPIBuilder.BuildOneOff(ko.Host(r), fn)
		specBytes, err := json.MarshalIndent(spec, "", "  ")
		if err == nil {
			lintResults, err := linter.LintSpec(specBytes)
			if err == nil {
				for _, result := range lintResults {
					res.Lints = append(res.Lints, fmt.Sprintf("[%s] %s (Line: %d)", result.RuleId, result.Message, result.StartNode.Line))
				}
			}
		}
	}

	return res, nil
}

func (s *RequestSniffer) calculatePaths(r *http.Request, patternPath string) (string, string, error) {
	requestPath := r.URL.Path

	if !s.BasePathRegex.MatchString(requestPath) {
		return "", "", fmt.Errorf("path %s must match %s", requestPath, s.BasePathRegex.String())
	}

	if patternPath == "" {
		return requestPath, requestPath, nil
	}

	if !s.ItemPathRegex.MatchString(patternPath) {
		return "", "", fmt.Errorf("pattern path %s must match %s", patternPath, s.ItemPathRegex.String())
	}

	match := s.ItemPathRegex.FindStringSubmatch(patternPath)
	namedGroups := make(map[string]string)
	if len(match) > 0 {
		for i, name := range s.ItemPathRegex.SubexpNames() {
			if i != 0 && name != "" {
				namedGroups[name] = match[i]
			}
		}
	}

	basePath := namedGroups["basePath"]

	if !strings.HasPrefix(patternPath, basePath) {
		return "", "", fmt.Errorf("pattern path %s must start with %s", patternPath, basePath)
	}

	// The pattern path must follow the net/http pattern rules
	if err := s.validatePattern(patternPath, r); err != nil {
		return "", "", err
	}

	return basePath, patternPath, nil
}

func (s *RequestSniffer) matchExisting(
	items []kdexv1alpha1.KDexFunction,
	name string,
	basePath string,
	routePath string,
	method string,
	operationId string,
) (*kdexv1alpha1.KDexFunction, bool) {
	for i := range items {
		fn := &items[i]

		if basePath != fn.Spec.API.BasePath {
			continue
		}

		if len(fn.Spec.API.Paths) == 0 {
			// a function matching by name but no api.paths is an exact match
			return fn, true
		}

		for curPath, curPathItem := range fn.Spec.API.Paths {
			existingOp := curPathItem.GetOp(method)

			// names match, we must return it
			if name == fn.Name {
				if (routePath == curPath) &&
					(existingOp != nil) &&
					(operationId == existingOp.OperationID) {

					return fn, true
				}

				return fn, false
			}

			if (routePath == curPath) &&
				(existingOp != nil) &&
				(operationId == existingOp.OperationID) {

				return fn, false
			}
		}
	}

	return nil, false
}

func (s *RequestSniffer) mergeAPIIntoFunction(
	out *kdexv1alpha1.KDexFunction,
	in map[string]*openapi.PathItem,
	schemas map[string]*openapi.SchemaRef,
	overwriteOperation bool,
	keepConflictedSchemas bool,
) {
	if out.Spec.API.Paths == nil {
		out.Spec.API.Paths = map[string]kdexv1alpha1.PathItem{}
	}

	for inPath, inItem := range in {
		outItem := out.Spec.API.Paths[inPath]

		if overwriteOperation {
			outItem.Description = inItem.Description
			outItem.Summary = inItem.Summary
		}

		outParams := outItem.GetParameters()
		for _, inParam := range inItem.Parameters {
			if inParam.Ref != "" {
				continue
			}
			found := false
			for odx, outParam := range outParams {
				if inParam.Value.Name == outParam.Name &&
					inParam.Value.In == outParam.In {
					if overwriteOperation {
						outParams[odx] = *inParam.Value
					} else {
						found = true
					}
					break
				}
			}
			if !found {
				outParams = append(outParams, *inParam.Value)
			}
		}
		outItem.SetParameters(outParams)

		for _, method := range kh.Methods() {
			op := getOp(string(method), inItem)
			if op != nil {
				outItem.SetOp(string(method), op)

				// If an op param is in shared (path) params, remove it from op
				for i := len(outItem.Parameters) - 1; i >= 0; i-- {
					if shouldDelete(outItem.GetParameters(), op.Parameters[i]) {
						op.Parameters = append(op.Parameters[:i], op.Parameters[i+1:]...)
					}
				}
			}
		}

		out.Spec.API.Paths[inPath] = outItem
	}

	// Merge schemas
	fnSchemas := out.Spec.API.GetSchemas()

	for key, schemaRef := range schemas {
		_, found := fnSchemas[key]
		if found && keepConflictedSchemas {
			key = key + ":conflict:" + rand.Text()[0:4]
		}

		fnSchemas[key] = schemaRef
	}

	out.Spec.API.SetSchemas(fnSchemas)
}

// nolint:gocyclo
func (s *RequestSniffer) parseRequestIntoAPI(
	r *http.Request,
	functionName string,
	routePath string,
	operationId string,
) (map[string]*openapi.PathItem, map[string]*openapi.SchemaRef, error) {
	pathItems := map[string]*openapi.PathItem{}
	schemas := map[string]*openapi.SchemaRef{}
	pathItem := &openapi.PathItem{
		Description: fmt.Sprintf("Auto-generated from request to %s", r.URL.Path),
	}

	op := &openapi.Operation{
		Deprecated:  strings.ToLower(r.Header.Get("X-KDex-Function-Deprecated")) == TRUE,
		OperationID: operationId,
		Parameters:  ko.ExtractParameters(routePath, r.URL.Query().Encode(), r.Header),
		Responses:   &openapi.Responses{},
	}

	tags := []string{}
	if tagsRaw := r.Header.Get("X-KDex-Function-Tags"); tagsRaw != "" {
		tags = strings.Split(tagsRaw, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}
	tags = append(tags, functionName)
	op.Tags = tags

	op.Summary = r.Header.Get("X-KDex-Function-Summary")
	if op.Summary == "" {
		op.Summary = operationId
	}

	op.Description = r.Header.Get("X-KDex-Function-Description")
	if op.Description == "" {
		op.Description = fmt.Sprintf("%s %s", r.Method, routePath)
	}

	requestSchemaRef := ""
	requestSchemaIsExternal := false

	if r.Header.Get("X-KDex-Function-Request-Schema-Ref") != "" {
		ref := r.Header.Get("X-KDex-Function-Request-Schema-Ref")
		requestSchemaIsExternal = urlSchemeRegex.MatchString(ref)

		if !requestSchemaIsExternal &&
			!strings.HasPrefix(ref, "#/components/schemas/") {

			ref = "#/components/schemas/" + ref
		}
		requestSchemaRef = ref
	}

	responseSchemaRef := ""
	responseSchemaIsExternal := false

	if r.Header.Get("X-KDex-Function-Response-Schema-Ref") != "" {
		ref := r.Header.Get("X-KDex-Function-Response-Schema-Ref")
		responseSchemaIsExternal = urlSchemeRegex.MatchString(ref)

		if !responseSchemaIsExternal &&
			!strings.HasPrefix(ref, "http") && !strings.HasPrefix(ref, "#/components/schemas/") {

			ref = "#/components/schemas/" + ref
		}
		responseSchemaRef = ref
	}

	// Authentication signal
	if r.Header.Get("Authorization") != "" {
		if len(*s.Security) > 0 {
			security := openapi.SecurityRequirements{}
			for _, mode := range *s.Security {
				security = append(security, mode)
			}
			op.Security = &security

			// Add 401 response when auth is required
			op.Responses.Set("401", &openapi.ResponseRef{
				Value: openapi.NewResponse().WithDescription("Unauthorized - Authentication required"),
			})
		}
	}

	// Process Request signals

	var body io.Reader = r.Body

	if r.ContentLength > 0 || r.Header.Get("Content-Type") != "" {
		contentType := strings.Split(r.Header.Get("Content-Type"), ";")[0]
		schema := openapi.NewSchema()
		var encoding map[string]*openapi.Encoding

		if !requestSchemaIsExternal {
			if contentType == "" {
				var err error
				var mt *mimetype.MIME
				mt, body, err = mime.Detect(body)

				if err != nil {
					return nil, nil, err
				}

				contentType = mt.String()
			}

			switch contentType {
			case "application/octet-stream":
				schema.Type = &openapi.Types{openapi.TypeString}
				schema.Format = "binary"
			case "multipart/form-data":
				mr, err := r.MultipartReader()
				if err != nil {
					return nil, nil, err
				}

				schema.Type = &openapi.Types{openapi.TypeObject}
				schema.Properties = openapi.Schemas{}
				encoding = map[string]*openapi.Encoding{}

				for {
					part, err := mr.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						return nil, nil, err
					}

					fieldName := part.FormName()

					_, isArray := schema.Properties[fieldName]

					if part.FileName() == "" {
						if isArray {
							schema.Properties[fieldName] = &openapi.SchemaRef{
								Value: &openapi.Schema{
									Type:  &openapi.Types{openapi.TypeArray},
									Items: openapi.NewStringSchema().NewRef(),
								},
							}
						} else {
							schema.Properties[fieldName] = openapi.NewStringSchema().NewRef()
						}
					} else {
						if isArray {
							schema.Properties[fieldName] = &openapi.SchemaRef{
								Value: &openapi.Schema{
									Type: &openapi.Types{openapi.TypeArray},
									Items: &openapi.SchemaRef{
										Value: &openapi.Schema{
											Type:   &openapi.Types{openapi.TypeString},
											Format: "binary",
										},
									},
								},
							}
							continue // no need to parse out the content type again
						} else {
							schema.Properties[fieldName] = &openapi.SchemaRef{
								Value: &openapi.Schema{
									Format: "binary",
									Type:   &openapi.Types{openapi.TypeString},
								},
							}
						}

						partContentType := part.Header.Get("Content-Type")

						if partContentType == "" || partContentType == "application/octet-stream" {
							var err error
							var mt *mimetype.MIME
							mt, _, err = mime.Detect(part)

							if err != nil {
								return nil, nil, err
							}

							partContentType = mt.String()
						}

						encoding[fieldName] = &openapi.Encoding{
							ContentType: partContentType,
						}
					}
				}
			case "application/x-www-form-urlencoded":
				_ = r.ParseForm()
				schema.Type = &openapi.Types{openapi.TypeObject}
				schema.Properties = openapi.Schemas{}
				for name := range r.PostForm {
					schema.Properties[name] = &openapi.SchemaRef{
						Value: openapi.NewStringSchema(),
					}
				}
			}

			if isJSON(contentType) {
				bytes, err := io.ReadAll(body)
				if err == nil {
					// Restore body for any subsequent uses
					// body = io.NopCloser(strings.NewReader(string(bytes)))

					var data any
					if err := json.Unmarshal(bytes, &data); err != nil {
						return nil, nil, err
					}

					schema = ko.InferSchema(data).Value
				}
			}
		}

		schema.Description = "Inferred from request body"

		op.RequestBody = &openapi.RequestBodyRef{
			Value: &openapi.RequestBody{
				Content:     openapi.NewContent(),
				Description: "The request body schema",
			},
		}

		var bodySchemaRef *openapi.SchemaRef

		if requestSchemaRef != "" {
			bodySchemaRef = &openapi.SchemaRef{
				Ref: requestSchemaRef,
			}

			schemaName, err := ko.ExtractSchemaName(requestSchemaRef)
			if err != nil {
				return nil, nil, err
			}

			if requestSchemaIsExternal {
				schemas[schemaName] = &openapi.SchemaRef{
					Ref: requestSchemaRef,
				}
			} else {
				schemas[schemaName] = &openapi.SchemaRef{
					Value: schema,
				}
			}
		} else {
			bodySchemaRef = &openapi.SchemaRef{
				Value: schema,
			}
		}

		op.RequestBody.Value.Content[contentType] = &openapi.MediaType{
			Schema:   bodySchemaRef,
			Encoding: encoding,
		}
	}

	// Process Response signals

	resp := openapi.NewResponse().WithDescription("Successful response")
	accept := r.Header.Get("Accept")

	if r.Method == "HEAD" || r.Method == "CONNECT" {
		resp.Content = openapi.NewContent()
	} else if accept != "" {
		content := openapi.NewContent()

		var schemaRef *openapi.SchemaRef

		if responseSchemaRef != "" {
			schemaRef = &openapi.SchemaRef{
				Ref: responseSchemaRef,
			}

			schemaName, err := ko.ExtractSchemaName(responseSchemaRef)
			if err != nil {
				return nil, nil, err
			}

			if responseSchemaIsExternal {
				schemas[schemaName] = &openapi.SchemaRef{
					Ref: responseSchemaRef,
				}
			}
		}

		// Split by comma and handle types like application/json;q=0.9
		types := strings.Split(accept, ",")
		for _, t := range types {
			mediaType := strings.TrimSpace(strings.Split(t, ";")[0])
			if mediaType == "" || mediaType == "*/*" {
				continue
			}
			content[mediaType] = &openapi.MediaType{
				Schema: schemaRef,
			}
		}

		if len(content) > 0 {
			resp.Content = content
		}
	}

	op.Responses.Set("200", &openapi.ResponseRef{
		Value: resp,
	})

	setOp(pathItem, r.Method, op)
	pathItems[routePath] = pathItem

	if strings.ToLower(r.Header.Get("X-KDex-Function-Comprehensive-Mode")) == TRUE {
		// Comprehensive Mode: Generate full CRUD suite
		// Determine base collection path and resource path
		var collectionPath, resourcePath string
		segments := strings.Split(routePath, "/")

		if len(segments) > 0 {
			lastSegment := segments[len(segments)-1]
			if strings.HasPrefix(lastSegment, "{") && strings.HasSuffix(lastSegment, "}") {
				// We are at a Resource Path (e.g., /v1/users/{id})
				resourcePath = routePath
				collectionPath = strings.Join(segments[:len(segments)-1], "/")
			} else {
				// We are at a Collection Path (e.g., /v1/users)
				collectionPath = routePath
				resourcePath = routePath + "/{id}"
			}

			// 1. Handle Collection Path (/resources)
			colItem, colExists := pathItems[collectionPath]
			if !colExists {
				colItem = &openapi.PathItem{
					Description: "Collection Operations",
				}
			}
			s.generateCollectionOperations(colItem, op, requestSchemaRef, responseSchemaRef)
			pathItems[collectionPath] = colItem

			// 2. Handle Resource Path (/resources/{id})
			resItem, resExists := pathItems[resourcePath]
			if !resExists {
				resItem = &openapi.PathItem{
					Description: "Resource Operations",
				}
			}
			s.generateResourceOperations(resourcePath, resItem, op, requestSchemaRef, responseSchemaRef)
			pathItems[resourcePath] = resItem
		}
	}

	return pathItems, schemas, nil
}

func (s *RequestSniffer) generateCollectionOperations(item *openapi.PathItem, templateOp *openapi.Operation, reqRef, respRef string) {
	// GET /resources (List)
	listOp := &openapi.Operation{
		Tags:        templateOp.Tags,
		Summary:     "List Resources",
		Description: "Retrieve a paginated list of resources.",
		OperationID: "list-" + templateOp.OperationID, // simplified ID generation
		Responses:   openapi.NewResponses(),
		Security:    templateOp.Security,
	}
	// Add pagination params
	listOp.Parameters = append(listOp.Parameters,
		&openapi.ParameterRef{Value: &openapi.Parameter{Name: "limit", In: "query", Description: "Maximum number of items to return.", Schema: openapi.NewSchemaRef("", openapi.NewIntegerSchema().WithMin(1).WithMax(100))}},
		&openapi.ParameterRef{Value: &openapi.Parameter{Name: "offset", In: "query", Description: "Offset to start returning items from.", Schema: openapi.NewSchemaRef("", openapi.NewIntegerSchema().WithMin(0))}},
	)

	listResp := openapi.NewResponse().WithDescription("List of resources")
	if respRef != "" {
		listResp.Content = openapi.NewContent()
		listResp.Content["application/json"] = &openapi.MediaType{
			Schema: &openapi.SchemaRef{
				Value: &openapi.Schema{
					Type:  &openapi.Types{openapi.TypeArray},
					Items: &openapi.SchemaRef{Ref: respRef},
				},
			},
		}
	}
	listOp.Responses.Set("200", &openapi.ResponseRef{Value: listResp})
	item.Get = listOp

	// POST /resources (Create)
	createOp := &openapi.Operation{
		Tags:        templateOp.Tags,
		Summary:     "Create Resource",
		Description: "Create a new resource.",
		OperationID: "create-" + templateOp.OperationID,
		Responses:   openapi.NewResponses(),
		Security:    templateOp.Security,
	}
	if reqRef != "" {
		createOp.RequestBody = &openapi.RequestBodyRef{
			Value: &openapi.RequestBody{
				Content:     openapi.NewContentWithSchemaRef(&openapi.SchemaRef{Ref: reqRef}, []string{"application/json"}),
				Description: "The resource to create",
			},
		}
	}

	createResp := openapi.NewResponse().WithDescription("Resource created")
	if respRef != "" {
		createResp.Content = openapi.NewContentWithSchemaRef(&openapi.SchemaRef{Ref: respRef}, []string{"application/json"})
	}
	createOp.Responses.Set("201", &openapi.ResponseRef{Value: createResp})
	item.Post = createOp
}

func (s *RequestSniffer) generateResourceOperations(resourcePath string, item *openapi.PathItem, templateOp *openapi.Operation, reqRef, respRef string) {
	// Extract path params from the resource path to ensure they are present
	pathParams := ko.ExtractParameters(resourcePath, "", http.Header{})

	// Add any non-path parameters from templateOp (e.g. headers, query from sniffed request)
	for _, p := range templateOp.Parameters {
		if p.Value != nil && p.Value.In != "path" {
			pathParams = append(pathParams, p)
		}
	}

	// GET /resources/{id} (Get)
	getOp := &openapi.Operation{
		Tags:        templateOp.Tags,
		Summary:     "Get Resource",
		Description: "Retrieve a single resource by ID.",
		OperationID: "get-" + templateOp.OperationID,
		Parameters:  pathParams,
		Responses:   openapi.NewResponses(),
		Security:    templateOp.Security,
	}
	getResp := openapi.NewResponse().WithDescription("Resource found")
	if respRef != "" {
		getResp.Content = openapi.NewContentWithSchemaRef(&openapi.SchemaRef{Ref: respRef}, []string{"application/json"})
	}
	getOp.Responses.Set("200", &openapi.ResponseRef{Value: getResp})
	item.Get = getOp

	// PUT /resources/{id} (Replace)
	putOp := &openapi.Operation{
		Tags:        templateOp.Tags,
		Summary:     "Replace Resource",
		Description: "Replace a resource by ID.",
		OperationID: "replace-" + templateOp.OperationID,
		Parameters:  pathParams,
		Responses:   openapi.NewResponses(),
		Security:    templateOp.Security,
	}
	if reqRef != "" {
		putOp.RequestBody = &openapi.RequestBodyRef{
			Value: &openapi.RequestBody{
				Content:     openapi.NewContentWithSchemaRef(&openapi.SchemaRef{Ref: reqRef}, []string{"application/json"}),
				Description: "The replaced resource",
			},
		}
	}
	putResp := openapi.NewResponse().WithDescription("Resource updated")
	if respRef != "" {
		putResp.Content = openapi.NewContentWithSchemaRef(&openapi.SchemaRef{Ref: respRef}, []string{"application/json"})
	}
	putOp.Responses.Set("200", &openapi.ResponseRef{Value: putResp})
	item.Put = putOp

	// PATCH /resources/{id} (Update)
	patchOp := &openapi.Operation{
		Tags:        templateOp.Tags,
		Summary:     "Update Resource",
		Description: "Partially update a resource by ID.",
		OperationID: "update-" + templateOp.OperationID,
		Parameters:  pathParams,
		Responses:   openapi.NewResponses(),
		Security:    templateOp.Security,
	}
	if reqRef != "" {
		patchOp.RequestBody = &openapi.RequestBodyRef{
			Value: &openapi.RequestBody{
				Content:     openapi.NewContentWithSchemaRef(&openapi.SchemaRef{Ref: reqRef}, []string{"application/merge-patch+json"}),
				Description: "The patch to apply",
			},
		}
	}
	patchResp := openapi.NewResponse().WithDescription("Resource updated")
	if respRef != "" {
		patchResp.Content = openapi.NewContentWithSchemaRef(&openapi.SchemaRef{Ref: respRef}, []string{"application/json"})
	}
	patchOp.Responses.Set("200", &openapi.ResponseRef{Value: patchResp})
	item.Patch = patchOp

	// DELETE /resources/{id} (Delete)
	deleteOp := &openapi.Operation{
		Tags:        templateOp.Tags,
		Summary:     "Delete Resource",
		Description: "Delete a resource by ID.",
		OperationID: "delete-" + templateOp.OperationID,
		Parameters:  pathParams,
		Responses:   openapi.NewResponses(),
		Security:    templateOp.Security,
	}
	deleteOp.Responses.Set("204", &openapi.ResponseRef{Value: openapi.NewResponse().WithDescription("Resource deleted")})
	item.Delete = deleteOp
}

// Basic criteria: non-HTML GET/POST requests that are not found
// (This is called when HostHandler hits a 404)
func (s *RequestSniffer) sniff(r *http.Request) (*kdexv1alpha1.KDexFunction, error) {
	// Skip internal paths
	if strings.HasPrefix(r.URL.Path, "/~") {
		return nil, nil
	}

	method := strings.ToUpper(r.Method)

	basePath, patternPath, err := s.calculatePaths(r, r.Header.Get("X-KDex-Function-Pattern-Path"))
	if err != nil {
		return nil, err
	}

	functionName := ko.GenerateNameFromPath(basePath, r.Header.Get("X-KDex-Function-Name"))
	patternName := ko.GenerateNameFromPath(patternPath, "")
	operationId := ko.GenerateOperationID(patternName, method, r.Header.Get("X-KDex-Function-Operation-ID"))

	// Check if a KDexFunction already exists for this path/method to avoid duplicates
	existing, exactMatch := s.matchExisting(s.Functions, functionName, basePath, patternPath, method, operationId)
	if existing != nil && !existing.Spec.Metadata.AutoGenerated {
		return existing, fmt.Errorf("the function %s/%s can no longer be targeted for autogeneration: .spec.metadata.autoGenerated=false", existing.Name, existing.Namespace)
	}
	if exactMatch && r.Header.Get("X-KDex-Function-Overwrite-Operation") != TRUE {
		return existing, fmt.Errorf("found an exact match for the operation on function %s/%s %s that is being skipped for safety: set X-KDex-Function-Overwrite-Operation: true to overwrite", method, existing.Name, existing.Namespace)
	}

	if existing != nil {
		functionName = existing.Name
	}

	pathItems, schemas, err := s.parseRequestIntoAPI(r, functionName, patternPath, operationId)

	if err != nil {
		return nil, err
	}

	fn := &kdexv1alpha1.KDexFunction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      functionName,
			Namespace: s.Namespace,
		},
	}

	if existing != nil {
		fn.Spec = existing.Spec
		if fn.Spec.Function.Language == "" {
			fn.Spec.Function.Language = "go"

		}
		if fn.Spec.Function.Entrypoint == "" {
			fn.Spec.Function.Environment = "go-env"
		}
	} else {
		fn.Spec = kdexv1alpha1.KDexFunctionSpec{
			API: kdexv1alpha1.API{
				BasePath: basePath,
				Paths:    map[string]kdexv1alpha1.PathItem{},
			},
			Function: kdexv1alpha1.KDexFunctionExec{
				Language:    "go",
				Environment: "go-env",
			},
			Metadata: kdexv1alpha1.KDexFunctionMetadata{
				AutoGenerated: true,
			},
		}
	}

	if !slices.Contains(fn.Spec.Metadata.Tags, fn.Name) {
		fn.Spec.Metadata.Tags = append(fn.Spec.Metadata.Tags, fn.Name)
	}

	s.mergeAPIIntoFunction(
		fn,
		pathItems,
		schemas,
		r.Header.Get("X-KDex-Function-Overwrite-Operation") != TRUE,
		r.Header.Get("X-KDex-Function-Keep-Schema-Conflict") == TRUE)

	return fn, nil
}

func (s *RequestSniffer) validatePattern(pattern string, r *http.Request) (err error) {
	// http.NewServeMux().HandleFunc panics if the pattern is invalid.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("invalid pattern path %q: %v", pattern, r)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {})
	_, matched := mux.Handler(r)
	if matched == "" {
		return fmt.Errorf("request %s %s does not align with pattern path %q", r.Method, r.URL.Path, pattern)
	}

	return nil
}

func getOp(method string, calcItem *openapi.PathItem) *openapi.Operation {
	switch kh.MethodFromString(method) {
	case kh.Connect:
		return calcItem.Connect
	case kh.Delete:
		return calcItem.Delete
	case kh.Get:
		return calcItem.Get
	case kh.Head:
		return calcItem.Head
	case kh.Options:
		return calcItem.Options
	case kh.Patch:
		return calcItem.Patch
	case kh.Post:
		return calcItem.Post
	case kh.Put:
		return calcItem.Put
	case kh.Trace:
		return calcItem.Trace
	}
	return nil
}

func isJSON(mimeType string) bool {
	return jsonMimeRegex.MatchString(strings.ToLower(mimeType))
}

func setOp(item *openapi.PathItem, method string, op *openapi.Operation) {
	switch kh.MethodFromString(method) {
	case kh.Connect:
		item.Connect = op
	case kh.Delete:
		item.Delete = op
	case kh.Get:
		item.Get = op
	case kh.Head:
		item.Head = op
	case kh.Options:
		item.Options = op
	case kh.Patch:
		item.Patch = op
	case kh.Post:
		item.Post = op
	case kh.Put:
		item.Put = op
	case kh.Trace:
		item.Trace = op
	}
}

func shouldDelete(parameters []openapi.Parameter, parameterRef *openapi.ParameterRef) bool {
	if parameterRef.Value == nil {
		return false
	}
	for _, commonParam := range parameters {
		if parameterRef.Value.Name == commonParam.Name && parameterRef.Value.In == commonParam.In {
			return true
		}
	}
	return false
}
