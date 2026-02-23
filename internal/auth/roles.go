package auth

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/go-ldap/ldap/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kdex-tech/kdex-host/internal"
	"github.com/oasdiff/yaml"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Lookup interface {
	FindInternal(subject string, password string) (bool, jwt.MapClaims, error)
	Type() string
}

type InternalIdentityProvider interface {
	FindInternal(subject string, password string) (jwt.MapClaims, error)
	FindInternalRolesAndEntitlements(subject string) ([]string, []string, error)
}

type scopeProvider struct {
	Client              client.Client
	Context             context.Context
	ControllerNamespace string
	FocalHost           string

	lookups  []Lookup
	rolesMap map[string][]string
}

var _ InternalIdentityProvider = (*scopeProvider)(nil)

func NewRoleProvider(
	ctx context.Context,
	c client.Client,
	focalHost string,
	controllerNamespace string,
	lookups []Lookup,
) (*scopeProvider, error) {
	rc := &scopeProvider{
		Client:              c,
		Context:             ctx,
		ControllerNamespace: controllerNamespace,
		FocalHost:           focalHost,
		lookups:             lookups,
	}

	roles, err := rc.collectRoles()
	if err != nil {
		return nil, err
	}

	rc.rolesMap = rc.buildMappingTable(roles)

	return rc, nil
}

func (rp *scopeProvider) FindInternal(subject string, password string) (jwt.MapClaims, error) {
	var localIdentity jwt.MapClaims
	for _, lookup := range rp.lookups {
		if ok, identity, err := lookup.FindInternal(subject, password); err != nil {
			return nil, err
		} else if ok {
			localIdentity = identity
			break
		}
	}

	if localIdentity == nil {
		return nil, fmt.Errorf("invalid credentials '%s'", subject)
	}

	roles, entitlements, err := rp.FindInternalRolesAndEntitlements(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve scopes: %w", err)
	}

	localIdentity["roles"] = roles
	localIdentity["entitlements"] = entitlements

	return localIdentity, nil
}

func (rp *scopeProvider) FindInternalRolesAndEntitlements(subject string) ([]string, []string, error) {
	roles, err := rp.resolveRoles(subject)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve roles: %w", err)
	}

	return roles, rp.collectEntitlements(roles), nil
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

func (rp *scopeProvider) collectEntitlements(roles []string) []string {
	scopes := []string{}
	for _, role := range roles {
		scopes = append(scopes, rp.rolesMap[role]...)
	}
	return scopes
}

func (rp *scopeProvider) buildMappingTable(roles *kdexv1alpha1.KDexRoleList) map[string][]string {
	table := map[string][]string{}

	for _, role := range roles.Items {
		table[role.Name] = []string{}

		for _, rule := range role.Spec.Rules {
			resourceNames := rule.ResourceNames

			if len(resourceNames) == 0 {
				resourceNames = []string{""}
			}

			for _, resource := range rule.Resources {
				for _, resourceName := range resourceNames {
					for _, verb := range rule.Verbs {
						table[role.Name] = append(table[role.Name], fmt.Sprintf("%s:%s:%s", resource, resourceName, verb))
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

type ldapLookup struct {
	activeDirectory   bool
	addr              string
	attributeMappings map[string]string
	attributeNames    []string
	baseDN            string
	bindUser          string // e.g., "cn=read-only-admin,dc=example,dc=com"
	bindPass          string
	userFilter        string // e.g., "(uid=%s)" or "(sAMAccountName=%s)"
}

var _ Lookup = (*ldapLookup)(nil)

func NewLDAPLookup(secret corev1.Secret) *ldapLookup {
	attributes := map[string]string{
		// default OpenLDAP attribute mappings
		"dn":             "sub",
		"uid":            "preferred_username",
		"cn":             "name",
		"givenName":      "given_name",
		"sn":             "surname",
		"mail":           "email",
		"email_verified": "email_verified",
		"memberOf":       "roles",
	}
	if string(secret.Data["active-directory"]) == TRUE {
		attributes = map[string]string{
			// default Active Directory attribute mappings
			"objectGUID":     "sub",
			"sAMAccountName": "preferred_username",
			"displayName":    "name",
			"givenName":      "given_name",
			"sn":             "surname",
			"mail":           "email",
			"emailVerified":  "email_verified",
			"memberOf":       "roles",
		}
	}
	if secret.Data["attributes"] != nil {
		for attr := range strings.SplitSeq(string(secret.Data["attributes"]), ",") {
			trimmed := strings.TrimSpace(attr)
			if _, ok := attributes[trimmed]; ok {
				continue
			}
			attributes[trimmed] = trimmed
		}
	}
	attributeNames := slices.Collect(maps.Keys(attributes))
	return &ldapLookup{
		addr:              string(secret.Data["addr"]),
		baseDN:            string(secret.Data["base-dn"]),
		bindUser:          string(secret.Data["bind-user"]),
		bindPass:          string(secret.Data["bind-pass"]),
		userFilter:        string(secret.Data["user-filter"]),
		attributeMappings: attributes,
		attributeNames:    attributeNames,
	}
}

func (ll *ldapLookup) FindInternal(subject string, password string) (bool, jwt.MapClaims, error) {
	// 1. Dial on every auth request (or use a pool)
	l, err := ldap.DialURL(ll.addr)
	if err != nil {
		return false, nil, fmt.Errorf("connection error: %w", err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			fmt.Printf("connection error: %v\n", err)
		}
	}()

	// 2. Bind with the pre-configured Service Account
	if err := l.Bind(ll.bindUser, ll.bindPass); err != nil {
		return false, nil, fmt.Errorf("service bind failed: %w", err)
	}

	// 3. Search for the user
	searchReq := ldap.NewSearchRequest(
		ll.baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf(ll.userFilter, ldap.EscapeFilter(subject)),
		ll.attributeNames,
		nil,
	)

	sr, err := l.Search(searchReq)
	if err != nil || len(sr.Entries) != 1 {
		return false, nil, nil
	}

	userEntry := sr.Entries[0]

	// 4. Verify user password
	if err := l.Bind(userEntry.DN, password); err != nil {
		return false, nil, fmt.Errorf("invalid password for subject '%s'", subject)
	}

	current := jwt.MapClaims{}
	for _, attr := range userEntry.Attributes {
		claimName, ok := ll.attributeMappings[attr.Name]
		if !ok {
			continue
		}

		// Special handling for Active Directory binary ID
		if ll.activeDirectory && attr.Name == "objectGUID" {
			raw := userEntry.GetRawAttributeValues("objectGUID")
			if len(raw) > 0 {
				guid, _ := FormatADGUID(raw[0])
				current[claimName] = guid
			}
			continue
		}

		if len(attr.Values) == 1 {
			// Most claims (email, name, sub) should be single strings
			current[claimName] = attr.Values[0]
		} else if len(attr.Values) > 1 {
			// Multi-value attributes (memberOf/roles) stay as slices
			current[claimName] = attr.Values
		}
	}

	return true, current, nil
}

func (ll *ldapLookup) Type() string {
	return "ldap"
}

type secretLookup struct {
	secrets kdexv1alpha1.ServiceAccountSecrets
}

var _ Lookup = (*secretLookup)(nil)

func NewSecretLookup(secrets kdexv1alpha1.ServiceAccountSecrets) *secretLookup {
	return &secretLookup{
		secrets: secrets.Filter(func(s corev1.Secret) bool { return s.Annotations["kdex.dev/secret-type"] == "subject" }),
	}
}

func (sl *secretLookup) FindInternal(subject string, password string) (bool, jwt.MapClaims, error) {
	for _, secret := range sl.secrets {
		if subBytes, ok := secret.Data["sub"]; ok && string(subBytes) == subject {
			if passBytes, ok := secret.Data["password"]; ok {
				if string(passBytes) == password || bcrypt.CompareHashAndPassword(passBytes, []byte(password)) == nil {
					current := map[string]any{}
					for k, bts := range secret.Data {
						if k == "password" {
							continue
						}
						var v any
						if err := yaml.Unmarshal(bts, &v); err != nil {
							current[k] = string(bts)
						} else {
							current[k] = v
						}
					}
					return true, current, nil
				}
			}
			return false, nil, fmt.Errorf("invalid password for subject '%s'", subject)
		}
	}

	return false, nil, nil
}

func (sl *secretLookup) Type() string {
	return "secret"
}

// FormatADGUID converts the raw binary objectGUID from AD into a standard UUID string.
// AD uses a little-endian format for the first three components.
func FormatADGUID(b []byte) (string, error) {
	if len(b) != 16 {
		return "", fmt.Errorf("invalid GUID length: %d", len(b))
	}

	// Byte flipping logic to match standard UUID string representation (RFC 4122)
	// AD GUID: [3 2 1 0] [5 4] [7 6] [8 9] [10 11 12 13 14 15]
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[3], b[2], b[1], b[0],
		b[5], b[4],
		b[7], b[6],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15]), nil
}
