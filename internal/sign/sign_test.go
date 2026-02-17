package sign_test

import (
	"crypto"
	"testing"
	"time"

	"github.com/kdex-tech/dmapper"
	"github.com/kdex-tech/kdex-host/internal/keys"
	"github.com/kdex-tech/kdex-host/internal/sign"
	"github.com/stretchr/testify/assert"
)

func TestNewSigner(t *testing.T) {
	tests := []struct {
		name       string
		audience   string
		duration   time.Duration
		issuer     string
		kid        string
		privateKey *crypto.Signer
		mapper     *dmapper.Mapper
		assertions func(*testing.T, *sign.Signer, error)
	}{
		{
			name:     "success",
			audience: "test",
			duration: time.Hour,
			issuer:   "test",
			kid:      "test",
			privateKey: func() *crypto.Signer {
				kp, err := keys.LoadKeyFromPEM([]byte(`-----BEGIN PRIVATE KEY-----
KID: kdex-dev-1769451504

MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgXufwXet+BRiqMQDn
7lWcoIgz6AVTAKOOJXlOz8JfxR2hRANCAASq6yLdpv9BkUW8SumvAkl+13QaAFDY
L51w6mkJ5U6GWpH1eZsXgKm0ZZJKEPsN9wYKe2LXT/WPpa5AwGzo7BLm
-----END PRIVATE KEY-----`))
				if err != nil {
					t.Fatal(err)
				}
				return &kp.Private
			}(),
			mapper: &dmapper.Mapper{},
			assertions: func(t *testing.T, s *sign.Signer, err error) {
				assert.NotNil(t, s)
				assert.Nil(t, err)
			},
		},
		{
			name: "missing audience",
			assertions: func(t *testing.T, s *sign.Signer, err error) {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "audience")
			},
		},
		{
			name:     "missing duration",
			audience: "test",
			duration: 0,
			assertions: func(t *testing.T, s *sign.Signer, err error) {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "duration")
			},
		},
		{
			name:     "missing issuer",
			audience: "test",
			duration: time.Hour,
			assertions: func(t *testing.T, s *sign.Signer, err error) {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "issuer")
			},
		},
		{
			name:     "missing kid",
			audience: "test",
			duration: time.Hour,
			issuer:   "test",
			privateKey: func() *crypto.Signer {
				kp, err := keys.LoadKeyFromPEM([]byte(`-----BEGIN PRIVATE KEY-----
KID: kdex-dev-1769451504

MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgXufwXet+BRiqMQDn
7lWcoIgz6AVTAKOOJXlOz8JfxR2hRANCAASq6yLdpv9BkUW8SumvAkl+13QaAFDY
L51w6mkJ5U6GWpH1eZsXgKm0ZZJKEPsN9wYKe2LXT/WPpa5AwGzo7BLm
-----END PRIVATE KEY-----`))
				if err != nil {
					t.Fatal(err)
				}
				return &kp.Private
			}(),
			assertions: func(t *testing.T, s *sign.Signer, err error) {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "key id")
			},
		},
		{
			name:       "missing private key",
			audience:   "test",
			duration:   time.Hour,
			issuer:     "test",
			kid:        "test",
			privateKey: nil,
			assertions: func(t *testing.T, s *sign.Signer, err error) {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "private key")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := sign.NewSigner(tt.audience, tt.duration, tt.issuer, tt.privateKey, tt.kid, tt.mapper)
			tt.assertions(t, got, gotErr)
		})
	}
}
