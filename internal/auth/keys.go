package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// PrivateKeySecretKey is the key in the secret that contains the private key
	PrivateKeySecretKey = "private-key"
	// PublicKeySecretKey is the key in the secret that contains the public key
	PublicKeySecretKey = "public-key"
)

var (
	instance *KeyPairs
	once     sync.Once
)

// KeyPair holds an RSA key pair for JWT signing and verification.
type KeyPair struct {
	ActiveKey bool
	KeyId     string
	Private   crypto.Signer
}

type KeyPairs []*KeyPair

func (p *KeyPairs) ActiveKey() *KeyPair {
	if p == nil {
		return nil
	}
	if len(*p) == 1 {
		return (*p)[0]
	}
	for _, pair := range *p {
		if pair.ActiveKey {
			return pair
		}
	}
	return nil
}

// LoadOrGenerateKeyPair loads an RSA key pair from a Kubernetes Secret.
// If the secret doesn't exist or is invalid, it generates a new key pair.
func LoadOrGenerateKeyPair(
	ctx context.Context,
	c client.Client,
	namespace string,
	jwt kdexv1alpha1.JWT,
	devMode bool,
) (*KeyPairs, error) {
	pairs := &KeyPairs{}
	found := false

	for _, secRef := range jwt.JWTKeysSecrets {
		var secret corev1.Secret
		err := c.Get(ctx, client.ObjectKey{
			Name:      secRef.SecretRef.Name,
			Namespace: namespace,
		}, &secret)

		if err != nil {
			return nil, err
		}

		kp, err := loadKeysFromSecret(&secret, jwt.ActiveKey)
		if err != nil {
			return nil, err
		}

		found = true
		*pairs = append(*pairs, kp)
	}

	if found {
		if len(*pairs) > 1 && pairs.ActiveKey() == nil {
			return nil, fmt.Errorf("multiple keys exist but none are specified as the active key via spec.auth.jwt.activeKey=%s", jwt.ActiveKey)
		}

		return pairs, nil
	}

	if devMode {
		return GenerateECDSAKeyPair(), nil
	}

	return nil, nil
}

// GenerateECDSAKeyPair generates a new ECDSA key pair for JWT signing.
func GenerateECDSAKeyPair() *KeyPairs {
	once.Do(func() {
		// 1. Use P-256 (ES256). It's lightning fast for dev restarts.
		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			// Panic because if we can't get entropy, we can't secure anything.
			panic(err)
		}

		// 2. Generate a unique ID based on the startup time.
		// This ensures clients/verifiers don't use a cached public key
		// from a previous process run.
		kid := fmt.Sprintf("kdex-dev-%d", time.Now().Unix())

		instance = &KeyPairs{
			{
				ActiveKey: true,
				KeyId:     kid,
				Private:   privateKey,
			},
		}
	})
	return instance
}

// loadKeysFromSecret loads an RSA key pair from a Kubernetes Secret.
func loadKeysFromSecret(secret *corev1.Secret, activeKey string) (*KeyPair, error) {
	privateKeyPEM, ok := secret.Data[PrivateKeySecretKey]
	if !ok {
		return nil, fmt.Errorf("secret does not contain %s", PrivateKeySecretKey)
	}

	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}

	var privKey any // Use 'any' (interface{}) to hold RSA or ECDSA
	var err error

	// 1. Try PKCS8 first (Modern standard, supports both RSA and ECDSA)
	privKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// 2. Fallback to PKCS1 (RSA Specific)
		privKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			// 3. Fallback to SEC1 (EC Specific)
			privKey, err = x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse private key in any format: %w", err)
			}
		}
	}

	// Optional: Validation. Ensure it's a type your app actually supports.
	switch privKey.(type) {
	case *rsa.PrivateKey, *ecdsa.PrivateKey:
		// Valid keys
	default:
		return nil, fmt.Errorf("unsupported private key type: %T", privKey)
	}

	signer, ok := privKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("key type %T does not implement crypto.Signer", privKey)
	}

	return &KeyPair{
		ActiveKey: activeKey == secret.Name,
		KeyId:     secret.Name,
		Private:   signer,
	}, nil
}
