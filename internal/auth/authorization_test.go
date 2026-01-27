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
		{
			name: "CheckPageAccess - no requirements",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				access, err := got.CheckPageAccess(context.Background(), nil, nil)
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - page requirements, no claims",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				access, err := got.CheckPageAccess(context.Background(), &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"pages"},
					},
				}, nil)
				assert.NoError(t, err)
				assert.False(t, access)
			},
		},
		{
			name: "CheckPageAccess - host requirements, no claims",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				access, err := got.CheckPageAccess(context.Background(), nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"pages"},
					},
				})
				assert.NoError(t, err)
				assert.False(t, access)
			},
		},
		{
			name: "CheckPageAccess - empty page or host requirements, with claims",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"users"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{},
					},
				}, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{},
					},
				})
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - page requirements, wrong coarse scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"users"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"pages"},
					},
				}, nil)
				assert.NoError(t, err)
				assert.False(t, access)
			},
		},
		{
			name: "CheckPageAccess - page requirements, right coarse scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"read"},
					},
				}, nil)
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - page requirements, action-resource exact scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:1:read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:1:read"},
					},
				}, nil)
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - page requirements, action-resource glob scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo::read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:1:read"},
					},
				}, nil)
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - page requirements, action-resource scope, short syntax",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:1:read"},
					},
				}, nil)
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - page requirements overrule host requirements",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:write"},
					},
				}, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:1:read"},
					},
				})
				assert.NoError(t, err)
				assert.False(t, access)
			},
		},
		{
			name: "CheckPageAccess - host requirements with glob requirements and specific scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:1:read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:read"},
					},
				})
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - host requirements with glob requirements and glob scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:read"},
					},
				})
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - requirements with specific requirements and specific scope, wrong name",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:1:read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:2:read"},
					},
				})
				assert.NoError(t, err)
				assert.False(t, access)
			},
		},
		{
			name: "CheckPageAccess - requirements with specific requirements and specific scope, wrong resource",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:1:read"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"bar:1:read"},
					},
				})
				assert.NoError(t, err)
				assert.False(t, access)
			},
		},
		{
			name: "CheckPageAccess - requirements with glob requirements and all verb scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:all"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:read"},
					},
				})
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - requirements with specific requirements and all verb scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:1:all"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:read"},
					},
				})
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - requirements with specific requirements and all verb scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:1:all"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:1:read"},
					},
				})
				assert.NoError(t, err)
				assert.True(t, access)
			},
		},
		{
			name: "CheckPageAccess - requirements with specific requirements and all verb scope",
			assertions: func(t *testing.T, got *AuthorizationChecker) {
				claims := &Claims{
					Scopes: []string{"foo:1:foo"},
				}
				ctx := context.WithValue(context.Background(), ClaimsContextKey, claims)
				access, err := got.CheckPageAccess(ctx, nil, &[]v1alpha1.SecurityRequirement{
					{
						"bearer": []string{"foo:1:read"},
					},
				})
				assert.NoError(t, err)
				assert.False(t, access)
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
