package auth

import (
	"context"
	"fmt"

	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RoleController struct {
	Client              client.Client
	Context             context.Context
	ControllerNamespace string
	FocalHost           string

	rolesMap map[string][]string
}

func NewRoleController(ctx context.Context, c client.Client, focalHost string, controllerNamespace string) (*RoleController, error) {
	rc := &RoleController{
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

func (rc *RoleController) CollectScopes(roles []string) []string {
	scopes := []string{}
	for _, role := range roles {
		scopes = append(scopes, rc.rolesMap[role]...)
	}
	return scopes
}

func (rc *RoleController) collectRoles() (*kdexv1alpha1.KDexRoleList, error) {
	var roles kdexv1alpha1.KDexRoleList
	if err := rc.Client.List(rc.Context, &roles, client.InNamespace(rc.ControllerNamespace), client.MatchingFields{
		internal.HOST_INDEX_KEY: rc.FocalHost,
	}); err != nil {
		return nil, err
	}
	return &roles, nil
}

func (rc *RoleController) buildMappingTable(roles *kdexv1alpha1.KDexRoleList) map[string][]string {
	table := map[string][]string{}

	for _, scope := range roles.Items {
		table[scope.Name] = []string{}

		for _, rule := range scope.Spec.Rules {
			resourceNames := rule.ResourceNames

			if len(resourceNames) == 0 {
				resourceNames = []string{""}
			}

			for _, resource := range rule.Resources {
				for _, resourceName := range rule.ResourceNames {
					for _, verb := range rule.Verbs {
						table[scope.Name] = append(table[scope.Name], fmt.Sprintf("%s:%s:%s", resource, resourceName, verb))
					}
				}
			}
		}
	}

	return table
}
