package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kdex.dev/crds/api/v1alpha1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestNewConfig(t *testing.T) {
	type testargs struct {
		c         client.Client
		auth      *kdexv1alpha1.Auth
		namespace string
		devMode   bool
	}

	tests := []struct {
		name       string
		args       testargs
		assertions func(t *testing.T, got *Config, goterr error)
	}{
		{
			name: "constructor, no auth",
			args: testargs{
				c:         nil,
				auth:      nil,
				namespace: "foo",
				devMode:   false,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.Nil(t, gotErr)
				assert.Equal(t, &Config{}, got)
			},
		},
		{
			name: "constructor, empty auth",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   false,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.Nil(t, gotErr)
				assert.Equal(t, &Config{CookieName: "auth_token", MappingRules: []CompiledMappingRule{}}, got)
			},
		},
		{
			name: "constructor, empty auth, devMode enabled, default TTL",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.Nil(t, gotErr)
				assert.NotNil(t, got.ActivePair)
				assert.NotNil(t, got.KeyPairs)
				assert.Equal(t, 1*time.Hour, got.TokenTTL)
			},
		},
		{
			name: "constructor, devMode enabled, invalid TTL",
			args: testargs{
				c: nil,
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						TokenTTL: "?",
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), "time: invalid duration")
			},
		},
		{
			name: "constructor, devMode enabled, with JWTKeysSecrets, secret not found",
			args: testargs{
				c: fake.NewClientBuilder().WithObjects().Build(),
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						JWTKeysSecrets: []kdexv1alpha1.LocalSecretWithKeyReference{
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "foo",
								},
							},
						},
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), `secrets "foo" not found`)
			},
		},
		{
			name: "constructor, devMode enabled, with JWTKeysSecrets, secret no matching key",
			args: testargs{
				c: fake.NewClientBuilder().WithObjects(&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: "foo",
					},
					StringData: map[string]string{
						"foo": "",
					},
				}).Build(),
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						JWTKeysSecrets: []kdexv1alpha1.LocalSecretWithKeyReference{
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "foo",
								},
							},
						},
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), `secret does not contain private-key`)
			},
		},
		{
			name: "constructor, devMode enabled, with JWTKeysSecrets, secret with invalid key",
			args: testargs{
				c: fake.NewClientBuilder().WithObjects(&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: "foo",
					},
					Data: map[string][]byte{
						"private-key": []byte(`-----BEGIN PRIVATE KEY-----
KID: kdex-dev-1769451504

MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgXufwXet+BRiqMQDn
7lWcoIgz6AVTAKOOJXlOz8Jf`),
					},
				}).Build(),
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						JWTKeysSecrets: []kdexv1alpha1.LocalSecretWithKeyReference{
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "foo",
								},
							},
						},
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), "failed to decode PEM block containing private key")
			},
		},
		{
			name: "constructor, devMode enabled, with JWTKeysSecrets, secret with matching key (ECDSA P-256)",
			args: testargs{
				c: fake.NewClientBuilder().WithObjects(&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: "foo",
					},
					Data: map[string][]byte{
						"private-key": []byte(`-----BEGIN PRIVATE KEY-----
KID: kdex-dev-1769451504

MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgXufwXet+BRiqMQDn
7lWcoIgz6AVTAKOOJXlOz8JfxR2hRANCAASq6yLdpv9BkUW8SumvAkl+13QaAFDY
L51w6mkJ5U6GWpH1eZsXgKm0ZZJKEPsN9wYKe2LXT/WPpa5AwGzo7BLm
-----END PRIVATE KEY-----`),
					},
				}).Build(),
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						JWTKeysSecrets: []kdexv1alpha1.LocalSecretWithKeyReference{
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "foo",
								},
							},
						},
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.Nil(t, gotErr)
				assert.NotNil(t, got.ActivePair)
				assert.Equal(t, 1, len(*got.KeyPairs))
			},
		},
		{
			name: "constructor, devMode enabled, with JWTKeysSecrets, secret with matching key (RSA)",
			args: testargs{
				c: fake.NewClientBuilder().WithObjects(&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: "foo",
					},
					Data: map[string][]byte{
						"private-key": []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAodh9j2EDujZ699rsSiqqv9oCItPSacdVlvDW7bwrkL3MzG3v
P2RUoU8FCg8JKiuqEq416a/DjWKcFaNg2semYoJXLTlwn+4X3zTIYoHCdQFRQ6MH
iUxy++Ty/zRGSVArZ0WH1tP8L828BYPqa9ljXSKS4ykn0L5kCBe1p/QB8/T8B/y1
+zAEt2uc8EUZVlDTrKCLP6/zubJtAmNaQuilGMnKzuMZ6S8VrJfc62b1r3SGO2X8
V3FxL6/WrqWko3jKemavM+5mGe0X5BZ9gSPM8pqlQkGhwhfoZf84bHYaW2E/uMhP
K8heKYRZJz122/LAxlGINvJDO8ubocdhrJ5JhQIDAQABAoIBAAzv/Ygpb6ms3Y3p
mgDgbJoofF+PV4nCzZ84F7OVBVXX2O1bOQhJZhB8/MCjQg7KbcPPhETEGu7hkUDo
41RUfa2bO1/EmzGq+o01BB2yag/TWqJ8VPJkl5PLkfcqP8Ia3qqt3rV4Evfj9iHq
ESJXlCn877P+oA2qN9yDv1mH17jKCJJo1+dNhcNWSeOA/JknguCwU0zY2whA0HZN
hDG4wp1LL+KcnhLETPP6Qvl5/ff82G2yMqpK5W+5VROSzqC86D84Nbp6iT4QGjbA
f08uLWimFS7bStgmvsch1WNBRJIZeTaslR0CoT3bV5CRyBJGLyDA9UY6pG68Hdmx
ezxG9xECgYEAz4GV9KOvw4cEhPNb0lwv4KZd3DQ1o4K2/8tMjpRkWx0tZnf2djkI
OEPW1eTrSeSuZTXtaEu+XDF/VgV/kuXrlqDpPV7kBdbmPt0GoplbCtmvZRrPbCzN
AKFvAs+CeG/OB9a6L89srn4Cv+SG5StWv3KRQOLR17VPvzaDbM5KRikCgYEAx6sv
amkUfGexH2B3Fs4Dh8+oxfAtuNbg8F+f0uC2XCyyvUTYCI2HRDWW0V38tk6wLyZT
vYAtKYCoAW9asB7dvgk1qcx+DAU6KN+Tfyau77bFtqxKA/ZxEJv/zT9j83WYL/OP
IWzF+TBzJ43aFnKPzTkQ7inrNJLUBNtMckUfu/0CgYBziHn+eLiey+j3QSvppsw9
b0OpHCSVQm0zZHTemb56gHdLqxU9Y6mw8gyGkOtz+/Ahh/ID9NArMp/sPCl4l60g
87yJH/EjUzBk5dkQ5QOsueEPEOtWFmeZp0hQr0q8VbvH34VQo1Omn6BWSR3WMNge
xeIb123whRG+q9Jm3UC7aQKBgDkbRNxyYWGTZp1KwcTL90aIpgS2xNzw2DTnpJZz
nrSONDDd18vabq2bhh8renPJ3aoelCTG3CPaoDKI3q8wpMsNZ0PBMOvPMustxsm/
DpmQ9MtiS2kGux+8/lR9pOCk6XoNdwpgSd8TdFwDvjRdX7OadrUnWBYZSHp7Hkow
avshAoGBAMtIw1LXeHrm4x7ngdRPEsyRQ2yKfvbHtgpIWtl9rcEQPoFC+slOlvoA
xY164RiE6GkAlFI0HwC6Xidg9xRgxNzAC70PjxKS9r2SVOZlsSpN3QE88CBZx62F
ZMtAm8mrV+h0ef/lr6zdJffz/EmM5MZrRAu2/dcK6S6qSEkwCTZ4
-----END RSA PRIVATE KEY-----`),
					},
				}).Build(),
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						JWTKeysSecrets: []kdexv1alpha1.LocalSecretWithKeyReference{
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "foo",
								},
							},
						},
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.Nil(t, gotErr)
				assert.NotNil(t, got.ActivePair)
				assert.Equal(t, 1, len(*got.KeyPairs))
			},
		},
		{
			name: "constructor, with JWTKeysSecrets, multiple keys, none selected as active",
			args: testargs{
				c: fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"private-key": []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAodh9j2EDujZ699rsSiqqv9oCItPSacdVlvDW7bwrkL3MzG3v
P2RUoU8FCg8JKiuqEq416a/DjWKcFaNg2semYoJXLTlwn+4X3zTIYoHCdQFRQ6MH
iUxy++Ty/zRGSVArZ0WH1tP8L828BYPqa9ljXSKS4ykn0L5kCBe1p/QB8/T8B/y1
+zAEt2uc8EUZVlDTrKCLP6/zubJtAmNaQuilGMnKzuMZ6S8VrJfc62b1r3SGO2X8
V3FxL6/WrqWko3jKemavM+5mGe0X5BZ9gSPM8pqlQkGhwhfoZf84bHYaW2E/uMhP
K8heKYRZJz122/LAxlGINvJDO8ubocdhrJ5JhQIDAQABAoIBAAzv/Ygpb6ms3Y3p
mgDgbJoofF+PV4nCzZ84F7OVBVXX2O1bOQhJZhB8/MCjQg7KbcPPhETEGu7hkUDo
41RUfa2bO1/EmzGq+o01BB2yag/TWqJ8VPJkl5PLkfcqP8Ia3qqt3rV4Evfj9iHq
ESJXlCn877P+oA2qN9yDv1mH17jKCJJo1+dNhcNWSeOA/JknguCwU0zY2whA0HZN
hDG4wp1LL+KcnhLETPP6Qvl5/ff82G2yMqpK5W+5VROSzqC86D84Nbp6iT4QGjbA
f08uLWimFS7bStgmvsch1WNBRJIZeTaslR0CoT3bV5CRyBJGLyDA9UY6pG68Hdmx
ezxG9xECgYEAz4GV9KOvw4cEhPNb0lwv4KZd3DQ1o4K2/8tMjpRkWx0tZnf2djkI
OEPW1eTrSeSuZTXtaEu+XDF/VgV/kuXrlqDpPV7kBdbmPt0GoplbCtmvZRrPbCzN
AKFvAs+CeG/OB9a6L89srn4Cv+SG5StWv3KRQOLR17VPvzaDbM5KRikCgYEAx6sv
amkUfGexH2B3Fs4Dh8+oxfAtuNbg8F+f0uC2XCyyvUTYCI2HRDWW0V38tk6wLyZT
vYAtKYCoAW9asB7dvgk1qcx+DAU6KN+Tfyau77bFtqxKA/ZxEJv/zT9j83WYL/OP
IWzF+TBzJ43aFnKPzTkQ7inrNJLUBNtMckUfu/0CgYBziHn+eLiey+j3QSvppsw9
b0OpHCSVQm0zZHTemb56gHdLqxU9Y6mw8gyGkOtz+/Ahh/ID9NArMp/sPCl4l60g
87yJH/EjUzBk5dkQ5QOsueEPEOtWFmeZp0hQr0q8VbvH34VQo1Omn6BWSR3WMNge
xeIb123whRG+q9Jm3UC7aQKBgDkbRNxyYWGTZp1KwcTL90aIpgS2xNzw2DTnpJZz
nrSONDDd18vabq2bhh8renPJ3aoelCTG3CPaoDKI3q8wpMsNZ0PBMOvPMustxsm/
DpmQ9MtiS2kGux+8/lR9pOCk6XoNdwpgSd8TdFwDvjRdX7OadrUnWBYZSHp7Hkow
avshAoGBAMtIw1LXeHrm4x7ngdRPEsyRQ2yKfvbHtgpIWtl9rcEQPoFC+slOlvoA
xY164RiE6GkAlFI0HwC6Xidg9xRgxNzAC70PjxKS9r2SVOZlsSpN3QE88CBZx62F
ZMtAm8mrV+h0ef/lr6zdJffz/EmM5MZrRAu2/dcK6S6qSEkwCTZ4
-----END RSA PRIVATE KEY-----`),
						},
					},
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "bar",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"private-key": []byte(`-----BEGIN PRIVATE KEY-----
KID: kdex-dev-1769451504

MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgXufwXet+BRiqMQDn
7lWcoIgz6AVTAKOOJXlOz8JfxR2hRANCAASq6yLdpv9BkUW8SumvAkl+13QaAFDY
L51w6mkJ5U6GWpH1eZsXgKm0ZZJKEPsN9wYKe2LXT/WPpa5AwGzo7BLm
-----END PRIVATE KEY-----`),
						},
					},
				).Build(),
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						JWTKeysSecrets: []kdexv1alpha1.LocalSecretWithKeyReference{
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "foo",
								},
							},
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "bar",
								},
							},
						},
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), "multiple keys exist but none are specified as the active key")
			},
		},
		{
			name: "constructor, with JWTKeysSecrets, multiple keys, one selected as active",
			args: testargs{
				c: fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"private-key": []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAodh9j2EDujZ699rsSiqqv9oCItPSacdVlvDW7bwrkL3MzG3v
P2RUoU8FCg8JKiuqEq416a/DjWKcFaNg2semYoJXLTlwn+4X3zTIYoHCdQFRQ6MH
iUxy++Ty/zRGSVArZ0WH1tP8L828BYPqa9ljXSKS4ykn0L5kCBe1p/QB8/T8B/y1
+zAEt2uc8EUZVlDTrKCLP6/zubJtAmNaQuilGMnKzuMZ6S8VrJfc62b1r3SGO2X8
V3FxL6/WrqWko3jKemavM+5mGe0X5BZ9gSPM8pqlQkGhwhfoZf84bHYaW2E/uMhP
K8heKYRZJz122/LAxlGINvJDO8ubocdhrJ5JhQIDAQABAoIBAAzv/Ygpb6ms3Y3p
mgDgbJoofF+PV4nCzZ84F7OVBVXX2O1bOQhJZhB8/MCjQg7KbcPPhETEGu7hkUDo
41RUfa2bO1/EmzGq+o01BB2yag/TWqJ8VPJkl5PLkfcqP8Ia3qqt3rV4Evfj9iHq
ESJXlCn877P+oA2qN9yDv1mH17jKCJJo1+dNhcNWSeOA/JknguCwU0zY2whA0HZN
hDG4wp1LL+KcnhLETPP6Qvl5/ff82G2yMqpK5W+5VROSzqC86D84Nbp6iT4QGjbA
f08uLWimFS7bStgmvsch1WNBRJIZeTaslR0CoT3bV5CRyBJGLyDA9UY6pG68Hdmx
ezxG9xECgYEAz4GV9KOvw4cEhPNb0lwv4KZd3DQ1o4K2/8tMjpRkWx0tZnf2djkI
OEPW1eTrSeSuZTXtaEu+XDF/VgV/kuXrlqDpPV7kBdbmPt0GoplbCtmvZRrPbCzN
AKFvAs+CeG/OB9a6L89srn4Cv+SG5StWv3KRQOLR17VPvzaDbM5KRikCgYEAx6sv
amkUfGexH2B3Fs4Dh8+oxfAtuNbg8F+f0uC2XCyyvUTYCI2HRDWW0V38tk6wLyZT
vYAtKYCoAW9asB7dvgk1qcx+DAU6KN+Tfyau77bFtqxKA/ZxEJv/zT9j83WYL/OP
IWzF+TBzJ43aFnKPzTkQ7inrNJLUBNtMckUfu/0CgYBziHn+eLiey+j3QSvppsw9
b0OpHCSVQm0zZHTemb56gHdLqxU9Y6mw8gyGkOtz+/Ahh/ID9NArMp/sPCl4l60g
87yJH/EjUzBk5dkQ5QOsueEPEOtWFmeZp0hQr0q8VbvH34VQo1Omn6BWSR3WMNge
xeIb123whRG+q9Jm3UC7aQKBgDkbRNxyYWGTZp1KwcTL90aIpgS2xNzw2DTnpJZz
nrSONDDd18vabq2bhh8renPJ3aoelCTG3CPaoDKI3q8wpMsNZ0PBMOvPMustxsm/
DpmQ9MtiS2kGux+8/lR9pOCk6XoNdwpgSd8TdFwDvjRdX7OadrUnWBYZSHp7Hkow
avshAoGBAMtIw1LXeHrm4x7ngdRPEsyRQ2yKfvbHtgpIWtl9rcEQPoFC+slOlvoA
xY164RiE6GkAlFI0HwC6Xidg9xRgxNzAC70PjxKS9r2SVOZlsSpN3QE88CBZx62F
ZMtAm8mrV+h0ef/lr6zdJffz/EmM5MZrRAu2/dcK6S6qSEkwCTZ4
-----END RSA PRIVATE KEY-----`),
						},
					},
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "bar",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"private-key": []byte(`-----BEGIN PRIVATE KEY-----
KID: kdex-dev-1769451504

MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgXufwXet+BRiqMQDn
7lWcoIgz6AVTAKOOJXlOz8JfxR2hRANCAASq6yLdpv9BkUW8SumvAkl+13QaAFDY
L51w6mkJ5U6GWpH1eZsXgKm0ZZJKEPsN9wYKe2LXT/WPpa5AwGzo7BLm
-----END PRIVATE KEY-----`),
						},
					},
				).Build(),
				auth: &kdexv1alpha1.Auth{
					JWT: kdexv1alpha1.JWT{
						ActiveKey: "bar",
						JWTKeysSecrets: []kdexv1alpha1.LocalSecretWithKeyReference{
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "foo",
								},
							},
							{
								KeyProperty: "private-key",
								SecretRef: v1.LocalObjectReference{
									Name: "bar",
								},
							},
						},
					},
				},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				assert.Nil(t, gotErr)
				assert.NotNil(t, got.ActivePair)
				assert.Equal(t, 2, len(*got.KeyPairs))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := NewConfig(context.Background(), tt.args.c, tt.args.auth, tt.args.namespace, tt.args.devMode)
			tt.assertions(t, got, gotErr)
		})
	}
}

func TestConfig_AddAuthentication(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(t.Output()), zap.UseDevMode(true)))

	type testargs struct {
		c         client.Client
		auth      *kdexv1alpha1.Auth
		namespace string
		devMode   bool
	}

	tests := []struct {
		name       string
		args       testargs
		assertions func(t *testing.T, got *Config, goterr error)
	}{
		{
			name: "authentication middleware skipped when no auth",
			args: testargs{
				c:         nil,
				auth:      nil,
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				handler := got.AddAuthentication(mux)
				assert.NotNil(t, handler)
				assert.True(t, mux == handler)
			},
		},
		{
			name: "authentication middleware added when auth",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				handler := got.AddAuthentication(mux)
				assert.NotNil(t, handler)
				assert.True(t, mux != handler)
			},
		},
		{
			name: "authentication - no header",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				mux.Handle("GET /foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
				}))
				handler := got.AddAuthentication(mux)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/foo", http.NoBody)
				handler.ServeHTTP(w, r)
				assert.Equal(t, 200, w.Code)
			},
		},
		{
			name: "authentication - Authorization header with bad token",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				mux.Handle("GET /foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
				}))
				handler := got.AddAuthentication(mux)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/foo", http.NoBody)
				r.Header.Set("Authorization", "Bearer foo")
				handler.ServeHTTP(w, r)
				assert.Equal(t, 401, w.Code)
				assert.Contains(t, w.Body.String(), "Invalid token")
			},
		},
		{
			name: "authentication - Authorization header with bad format",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				mux.Handle("GET /foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
				}))
				handler := got.AddAuthentication(mux)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/foo", http.NoBody)
				r.Header.Set("Authorization", "Bearer foo bar")
				handler.ServeHTTP(w, r)
				assert.Equal(t, 401, w.Code)
				assert.Contains(t, w.Body.String(), "Invalid Authorization header format")
			},
		},
		{
			name: "authentication - Cookie invalid token",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				mux.Handle("GET /foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
				}))
				handler := got.AddAuthentication(mux)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/foo", http.NoBody)
				r.Header.Set("Cookie", "auth_token=foo")
				handler.ServeHTTP(w, r)
				assert.Equal(t, 303, w.Code)
				assert.Contains(t, w.Body.String(), "See Other")
			},
		},
		{
			name: "authentication - Authorization header signed token",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				mux.Handle("GET /foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
				}))
				handler := got.AddAuthentication(mux)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/foo", http.NoBody)

				token, err := SignToken("foo", "foo@foo.bar", got.ClientID, "issuer", []string{}, nil, got.ActivePair, got.TokenTTL)
				assert.Nil(t, err)

				r.Header.Set("Authorization", "Bearer "+token)
				handler.ServeHTTP(w, r)
				assert.Equal(t, 200, w.Code)
			},
		},
		{
			name: "authentication - Cookie signed token",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				mux.Handle("GET /foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
				}))
				handler := got.AddAuthentication(mux)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/foo", http.NoBody)

				token, err := SignToken("foo", "foo@foo.bar", got.ClientID, "issuer", []string{}, nil, got.ActivePair, got.TokenTTL)
				assert.Nil(t, err)

				r.Header.Set("Cookie", "auth_token="+token)
				handler.ServeHTTP(w, r)
				assert.Equal(t, 200, w.Code)
			},
		},
		{
			name: "authentication - Cookie token expired",
			args: testargs{
				c:         nil,
				auth:      &kdexv1alpha1.Auth{},
				namespace: "foo",
				devMode:   true,
			},
			assertions: func(t *testing.T, got *Config, gotErr error) {
				mux := http.NewServeMux()
				mux.Handle("GET /foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(200)
				}))
				handler := got.AddAuthentication(mux)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/foo", http.NoBody)

				token, err := SignToken("foo", "foo@foo.bar", got.ClientID, "issuer", []string{}, nil, got.ActivePair, 1*time.Microsecond)
				assert.Nil(t, err)

				r.Header.Set("Cookie", "auth_token="+token)
				handler.ServeHTTP(w, r)
				assert.Equal(t, 303, w.Code)
				assert.Contains(t, w.Body.String(), "See Other")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := NewConfig(context.Background(), tt.args.c, tt.args.auth, tt.args.namespace, tt.args.devMode)
			tt.assertions(t, got, gotErr)
		})
	}
}

func TestConfig_OIDC(t *testing.T) {
	extra := map[string]any{
		"firstName": "Joe",
		"lastName":  "Bar",
		"address": map[string]any{
			"street":  "1 Long Dr",
			"city":    "Baytown",
			"country": "Supernautica",
		},
	}
	scopeProvider := &mockScopeProvider{
		verifyLocalIdentity: func(subject string, password string) (*Identity, error) {
			if subject == "not-allowed" {
				return nil, fmt.Errorf("invalid credentials")
			}

			return &Identity{
				Email:   subject,
				Extra:   extra,
				Subject: subject,
				Scopes:  []string{"foo", "bar"},
			}, nil
		},
		resolveScopes: func(subject string) ([]string, error) {
			return nil, nil
		},
	}

	tests := []struct {
		name       string
		cfg        func(string) (Config, error)
		sp         ScopeProvider
		assertions func(t *testing.T, serverURL string)
	}{
		{
			name: "OIDC - constructor, no client id",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string) {
				client := fake.NewClientBuilder().WithObjects().Build()
				_, gotErr := NewConfig(context.Background(), client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						OIDCProviderURL: "http://bad",
					},
				}, "foo", true)
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), "there is no client id configured in")
			},
		},
		{
			name: "OIDC - constructor, no secret defined",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string) {
				client := fake.NewClientBuilder().WithObjects().Build()
				_, gotErr := NewConfig(context.Background(), client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID:        "foo",
						OIDCProviderURL: "http://bad",
					},
				}, "foo", true)
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), "there is no Secret containing the OIDC client_secret configured")
			},
		},
		{
			name: "OIDC - constructor, secret defined but missing",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string) {
				client := fake.NewClientBuilder().WithObjects().Build()
				_, gotErr := NewConfig(context.Background(), client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: "http://bad",
					},
				}, "foo", true)
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), `secrets "foo" not found`)
			},
		},
		{
			name: "OIDC - constructor, secret defined but missing key",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string) {
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						StringData: map[string]string{
							"foo": "bar",
						},
					},
				).Build()
				_, gotErr := NewConfig(context.Background(), client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: "http://bad",
					},
				}, "foo", true)
				assert.NotNil(t, gotErr)
				assert.Contains(t, gotErr.Error(), `secret foo/foo does not contain the key`)
			},
		},
		{
			name: "OIDC - constructor, secret defined, valid key",
			sp:   scopeProvider,
			assertions: func(t *testing.T, serverURL string) {
				client := fake.NewClientBuilder().WithObjects(
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "foo",
						},
						Data: map[string][]byte{
							"client_secret": []byte("bar"),
						},
					},
				).Build()
				cfg, gotErr := NewConfig(context.Background(), client, &v1alpha1.Auth{
					OIDCProvider: &v1alpha1.OIDCProvider{
						ClientID: "foo",
						ClientSecretRef: v1alpha1.LocalSecretWithKeyReference{
							KeyProperty: "client_secret",
							SecretRef: v1.LocalObjectReference{
								Name: "foo",
							},
						},
						OIDCProviderURL: "http://bad",
					},
				}, "foo", true)
				assert.Nil(t, gotErr)
				assert.Equal(t, "bar", cfg.ClientSecret)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assertions(t, "http://foo")
		})
	}
}
