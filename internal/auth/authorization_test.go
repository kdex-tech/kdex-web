package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"kdex.dev/crds/api/v1alpha1"
)

func TestNewAuthorizationChecker(t *testing.T) {
	tests := []struct {
		name       string
		assertions func(t *testing.T, got *AuthorizationChecker)
	}{
		{
			name: "constructor",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				assert.Equal(t, &AuthorizationChecker{anonymousGrants: []string{}}, got)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAuthorizationChecker([]string{})
			tt.assertions(t, got)
		})
	}
}

func TestAuthorizationChecker_CheckAccess(t *testing.T) {
	tests := []struct {
		name            string
		kind            string
		resourceName    string
		claims          *Claims
		req             []v1alpha1.SecurityRequirement
		anonymousGrants []string
		succeeds        bool
	}{
		{
			name:         "CheckPageAccess - claims= / req=[]",
			kind:         "pages",
			resourceName: "1",
			claims:       nil,
			req:          nil,
			succeeds:     false,
		},
		{
			name:            "CheckPageAccess - claims= / req=[] + anon",
			kind:            "pages",
			resourceName:    "1",
			claims:          nil,
			req:             nil,
			anonymousGrants: []string{"pages:read"},
			succeeds:        true,
		},
		{
			name:         "CheckPageAccess - claims= / req=[{bearer:[pages]}]",
			kind:         "pages",
			resourceName: "1",
			claims:       nil,
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims= / req=[{bearer:[pages]}] + anon",
			kind:         "pages",
			resourceName: "1",
			claims:       nil,
			req: []v1alpha1.SecurityRequirement{
				{
					// opaque scope doesn't match
					"bearer": []string{"pages"},
				},
			},
			anonymousGrants: []string{"pages:read"},
			succeeds:        false,
		},
		{
			name:         "CheckPageAccess - claims=pages / req=[{bearer:[pages]}] + anon",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages"},
				},
			},
			anonymousGrants: []string{"pages:read"},
			succeeds:        true,
		},
		{
			name:         "CheckPageAccess - claims=pages / req=[{bearer:[]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=users / req=[{bearer:[pages]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"users"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=read / req=[{beader:[read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"read"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=read / req=[{beader:[read]}] + anon",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"read"},
				},
			},
			anonymousGrants: []string{"pages:read"},
			succeeds:        true,
		},
		{
			name:         "CheckPageAccess - claims=pages:1:read / req=[{bearer:[pages:1:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:1:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=pages::read / req=[{bearer:[pages:1:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages::read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:1:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=pages:read / req=[{bearer:[pages:1:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:1:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=pages:1:read / req=[{bearer:[pages:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=pages:read / req=[{bearer:[pages:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=pages:1:read / req=[{bearer:[pages:2:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:2:read"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=pages:1:read / req=[{bearer:[bar:1:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"bar:1:read"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=pages:all / req=[{bearer:[pages:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:all"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=pages:all / req=[{bearer:[pages:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:all"},
			},
			req:      []v1alpha1.SecurityRequirement{},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=pages:1:all / req=[{bearer:[pages:read]}]",
			kind:         "pages",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"pages:1:all"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=foo:1:all / req=[{bearer:[foo:1:all]}]",
			kind:         "foo",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"foo:1:all"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=foo:1:foo / req=[{bearer:[foo:1:read]}]",
			kind:         "foo",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"foo:1:foo"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=foo:1:read / req=[{bearer:[foo:1:read]}{oauth:[admin]}]",
			kind:         "foo",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"foo:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
				},
				{
					"oauth": []string{"admin"},
				},
			},
			succeeds: true,
		},
		{
			name:         "CheckPageAccess - claims=admin / req=[{bearer:[foo:1:read]}{oauth:[admin]}]",
			kind:         "foo",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"admin"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
				},
				{
					"oauth": []string{"admin"},
				},
			},
			anonymousGrants: []string{"foo:read"},
			succeeds:        true,
		},
		{
			name:         "CheckPageAccess - claims=foo:1:read / req=[{bearer:[foo:1:read],oauth:[admin]}]",
			kind:         "foo",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"foo:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
					"oauth":  []string{"admin"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=admin / req=[{bearer:[foo:1:read],oauth:[admin]}]",
			kind:         "foo",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"admin"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
					"oauth":  []string{"admin"},
				},
			},
			succeeds: false,
		},
		{
			name:         "CheckPageAccess - claims=foo:1:read,admin / req=[{bearer:[foo:1:read],oauth:[admin]}]",
			kind:         "foo",
			resourceName: "1",
			claims: &Claims{
				Scopes: []string{"foo:1:read", "admin"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
					"oauth":  []string{"admin"},
				},
			},
			succeeds: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.claims != nil {
				ctx = context.WithValue(context.Background(), ClaimsContextKey, tt.claims)
			}
			checker := NewAuthorizationChecker(tt.anonymousGrants)
			access, err := checker.CheckAccess(
				ctx, tt.kind, tt.resourceName, tt.req)
			if assert.NoError(t, err) {
				assert.Equal(t, tt.succeeds, access)
			}
		})
	}
}
