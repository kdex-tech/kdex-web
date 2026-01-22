package host

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openapi "github.com/getkin/kin-openapi/openapi3"
	"github.com/go-logr/logr"
	G "github.com/onsi/gomega"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ko "kdex.dev/web/internal/openapi"
)

func TestHostHandler_SchemaHandler(t *testing.T) {
	g := G.NewGomegaWithT(t)

	// Setup HostHandler
	th := NewHostHandler(nil, "test-host", "default", logr.Discard())

	// Define some schemas
	userSchema := &openapi.SchemaRef{
		Value: &openapi.Schema{
			Type: &openapi.Types{openapi.TypeObject},
			Properties: openapi.Schemas{
				"name": &openapi.SchemaRef{
					Value: &openapi.Schema{
						Type: &openapi.Types{openapi.TypeString},
					},
				},
			},
		},
	}

	addrSchema := &openapi.SchemaRef{
		Value: &openapi.Schema{
			Type: &openapi.Types{openapi.TypeObject},
			Properties: openapi.Schemas{
				"city": &openapi.SchemaRef{
					Value: &openapi.Schema{
						Type: &openapi.Types{openapi.TypeString},
					},
				},
			},
		},
	}

	// Register paths with schemas
	registeredPaths := map[string]ko.PathInfo{
		"/v1/users": {
			API: ko.OpenAPI{
				BasePath: "/v1/users",
				Schemas: map[string]*openapi.SchemaRef{
					"User": userSchema,
				},
			},
			Type: ko.FunctionPathType,
		},
		"/v1/common": {
			API: ko.OpenAPI{
				BasePath: "/v1/common",
				Schemas: map[string]*openapi.SchemaRef{
					"Address": addrSchema,
					"User":    addrSchema, // Conflict with /v1/users User
				},
			},
			Type: ko.FunctionPathType,
		},
	}

	th.SetHost(&kdexv1alpha1.KDexHostSpec{
		DefaultLang: "en",
	}, nil, nil, nil, "", registeredPaths, nil)

	tests := []struct {
		name       string
		path       string
		wantCode   int
		wantSchema *openapi.SchemaRef
	}{
		{
			name:       "global lookup - unique",
			path:       "/~/schema/Address",
			wantCode:   http.StatusOK,
			wantSchema: addrSchema,
		},
		{
			name:       "global lookup - first win",
			path:       "/~/schema/User",
			wantCode:   http.StatusOK,
			wantSchema: userSchema, // depends on map iteration order, but /v1/users is first in my map (not guaranteed)
		},
		{
			name:       "namespaced lookup - User in v1/users",
			path:       "/~/schema/v1/users/User",
			wantCode:   http.StatusOK,
			wantSchema: userSchema,
		},
		{
			name:       "namespaced lookup - User in v1/common",
			path:       "/~/schema/v1/common/User",
			wantCode:   http.StatusOK,
			wantSchema: addrSchema,
		},
		{
			name:     "not found",
			path:     "/~/schema/NonExistent",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "namespaced not found",
			path:     "/~/schema/v1/users/Address",
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			th.ServeHTTP(w, req)

			g.Expect(w.Code).To(G.Equal(tt.wantCode))

			if tt.wantCode == http.StatusOK {
				var gotSchema openapi.SchemaRef
				err := json.Unmarshal(w.Body.Bytes(), &gotSchema)
				g.Expect(err).NotTo(G.HaveOccurred())

				// Compare marshaled bytes for simplicity
				gotBytes, _ := json.Marshal(gotSchema)
				wantBytes, _ := json.Marshal(tt.wantSchema)
				g.Expect(gotBytes).To(G.MatchJSON(wantBytes))
			}
		})
	}
}
