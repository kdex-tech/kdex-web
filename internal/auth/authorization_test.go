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
				assert.Equal(t, &AuthorizationChecker{}, got)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAuthorizationChecker()
			tt.assertions(t, got)
		})
	}
}

func TestAuthorizationChecker_CheckPageAccess(t *testing.T) {
	tests := []struct {
		name     string
		claims   *Claims
		req      []v1alpha1.SecurityRequirement
		succeeds bool
	}{
		{
			name:     "CheckPageAccess - claims= / req=[]",
			claims:   &Claims{},
			req:      nil,
			succeeds: true,
		},
		{
			name:   "CheckPageAccess - claims= / req=[{bearer:[pages]}]",
			claims: &Claims{},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"pages"},
				},
			},
			succeeds: false,
		},
		{
			name: "CheckPageAccess - claims=users / req=[{bearer:[]}]",
			claims: &Claims{
				Scopes: []string{"users"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=users / req=[{bearer:[pages]}]",
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
			name: "CheckPageAccess - claims=read / req=[{beader:[read]}]",
			claims: &Claims{
				Scopes: []string{"read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo:1:read / req=[{bearer:[foo:1:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo::read / req=[{bearer:[foo:1:read]}]",
			claims: &Claims{
				Scopes: []string{"foo::read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo:read / req=[{bearer:[foo:1:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:1:read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo:1:read / req=[{bearer:[foo:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo:read / req=[{bearer:[foo:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo:1:read / req=[{bearer:[foo:2:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:2:read"},
				},
			},
			succeeds: false,
		},
		{
			name: "CheckPageAccess - claims=foo:1:read / req=[{bearer:[bar:1:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:1:read"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"bar:1:read"},
				},
			},
			succeeds: false,
		},
		{
			name: "CheckPageAccess - claims=foo:all / req=[{bearer:[foo:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:all"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo:1:all / req=[{bearer:[foo:read]}]",
			claims: &Claims{
				Scopes: []string{"foo:1:all"},
			},
			req: []v1alpha1.SecurityRequirement{
				{
					"bearer": []string{"foo:read"},
				},
			},
			succeeds: true,
		},
		{
			name: "CheckPageAccess - claims=foo:1:all / req=[{bearer:[foo:1:all]}]",
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
			name: "CheckPageAccess - claims=foo:1:foo / req=[{bearer:[foo:1:read]}]",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), ClaimsContextKey, tt.claims)
			checker := NewAuthorizationChecker()
			access, err := checker.CheckPageAccess(ctx, tt.req)
			assert.NoError(t, err)
			assert.Equal(t, tt.succeeds, access)
		})
	}
}
