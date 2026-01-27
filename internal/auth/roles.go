package auth

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Identity struct {
	Email   string
	Extra   map[string]any
	Subject string
	Scopes  []string
}

type ScopeProvider interface {
	ResolveIdentity(subject string, password string, identity *Identity) error
	ResolveScopes(subject string) ([]string, error)
	VerifyLocalIdentity(subject string, password string) (*Identity, error)
}

type scopeProvider struct {
	Client              client.Client
	Context             context.Context
	ControllerNamespace string
	FocalHost           string

	rolesMap map[string][]string
}

func NewRoleProvider(ctx context.Context, c client.Client, focalHost string, controllerNamespace string) (*scopeProvider, error) {
	rc := &scopeProvider{
		Client:              c,
		Context:             ctx,
		ControllerNamespace: controllerNamespace,
		FocalHost:           focalHost,
	}

	roles, err := rc.collectRoles()
	if err != nil {
		return nil, err
	}

	rc.rolesMap = rc.buildMappingTable(roles)

	return rc, nil
}

func (rp *scopeProvider) ResolveIdentity(subject string, password string, identity *Identity) error {
	bindings, err := rp.resolveBindings(subject)
	if err != nil {
		return err
	}
	if len(bindings.Items) == 0 {
		return fmt.Errorf("invalid credentials: no binding")
	}

	// TODO: implement external lookup like LDAP

	passwordValid := false
	for _, binding := range bindings.Items {
		if binding.Spec.SecretRef != nil {
			var secret corev1.Secret
			if err := rp.Client.Get(rp.Context, client.ObjectKey{
				Name:      binding.Spec.SecretRef.Name,
				Namespace: binding.Namespace,
			}, &secret); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed checking secret for binding %s/%s: %w", binding.Namespace, binding.Name, err)
			}
			passBytes, ok := secret.Data[subject]
			if ok && string(passBytes) == password {
				passwordValid = true
				identity.Email = binding.Spec.Email
				identity.Subject = subject
				break
			}
		}
	}

	if !passwordValid {
		return fmt.Errorf("invalid credentials: no secret")
	}

	return nil
}

func (rp *scopeProvider) ResolveScopes(subject string) ([]string, error) {
	roles, err := rp.resolveRoles(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve roles: %w", err)
	}

	return rp.collectScopes(roles), nil
}

func (rp *scopeProvider) VerifyLocalIdentity(subject string, password string) (*Identity, error) {
	localIdentity := &Identity{
		Email:   subject,
		Subject: subject,
	}

	err := rp.ResolveIdentity(subject, password, localIdentity)
	if err != nil {
		return nil, fmt.Errorf("invalid identity %w", err)
	}

	scopes, err := rp.ResolveScopes(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve scopes: %w", err)
	}

	localIdentity.Scopes = scopes

	return localIdentity, nil
}

func (rp *scopeProvider) collectRoles() (*kdexv1alpha1.KDexRoleList, error) {
	var roles kdexv1alpha1.KDexRoleList
	if err := rp.Client.List(rp.Context, &roles, client.InNamespace(rp.ControllerNamespace), client.MatchingFields{
		internal.HOST_INDEX_KEY: rp.FocalHost,
	}); err != nil {
		return nil, err
	}
	return &roles, nil
}

func (rp *scopeProvider) collectScopes(roles []string) []string {
	scopes := []string{}
	for _, role := range roles {
		scopes = append(scopes, rp.rolesMap[role]...)
	}
	return scopes
}

func (rp *scopeProvider) buildMappingTable(roles *kdexv1alpha1.KDexRoleList) map[string][]string {
	table := map[string][]string{}

	for _, scope := range roles.Items {
		table[scope.Name] = []string{}

		for _, rule := range scope.Spec.Rules {
			resourceNames := rule.ResourceNames

			if len(resourceNames) == 0 {
				resourceNames = []string{""}
			}

			for _, resource := range rule.Resources {
				for _, resourceName := range resourceNames {
					for _, verb := range rule.Verbs {
						table[scope.Name] = append(table[scope.Name], fmt.Sprintf("%s:%s:%s", resource, resourceName, verb))
					}
				}
			}
		}
	}

	return table
}

func (rp *scopeProvider) resolveBindings(subject string) (*kdexv1alpha1.KDexRoleBindingList, error) {
	var roleBindings kdexv1alpha1.KDexRoleBindingList
	if err := rp.Client.List(rp.Context, &roleBindings, client.InNamespace(rp.ControllerNamespace), client.MatchingFields{
		internal.HOST_INDEX_KEY: rp.FocalHost,
		internal.SUB_INDEX_KEY:  subject,
	}); err != nil {
		return nil, err
	}

	// TODO: I think roleBindings are supposed to support regex "subject" such that the bindings may apply to antire
	// class of users.

	return &roleBindings, nil
}

func (rp *scopeProvider) resolveRoles(subject string) ([]string, error) {
	var roles []string

	bindings, err := rp.resolveBindings(subject)
	if err != nil {
		return roles, err
	}
	if len(bindings.Items) == 0 {
		return roles, nil
	}

	for _, policy := range bindings.Items {
		// Generalized sub matching: exact match for now, can be extended to regex
		if policy.Spec.Subject == subject || policy.Spec.Subject == "*" {
			roles = append(roles, policy.Spec.Roles...)
		}
	}

	return roles, nil
}
