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
	var secret corev1.Secret

	for _, secRef := range jwt.JWTKeysSecrets {
		err := c.Get(ctx, client.ObjectKey{
			Name:      secRef.SecretRef.Name,
			Namespace: namespace,
		}, &secret)

		if client.IgnoreNotFound(err) != nil {
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
		return pairs, nil
	}

	if devMode {
		// Generate new key pair
		return GenerateKeyPair(), nil
	}

	return nil, nil
}

// GenerateKeyPair generates a new RSA key pair for JWT signing.
func GenerateKeyPair() *KeyPairs {
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

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is not RSA private key")
		}
	}

	return &KeyPair{
		ActiveKey: activeKey == secret.Name,
		KeyId:     secret.Name,
		Private:   privateKey,
	}, nil
}

// ExportToSecret exports the key pair to a format suitable for storing in a Kubernetes Secret.
// func (kp *KeyPair) ExportToSecret() (map[string][]byte, error) {
// 	privateKeyBytes := x509.MarshalPKCS1PrivateKey(kp.Private)
// 	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
// 		Type:  "RSA PRIVATE KEY",
// 		Bytes: privateKeyBytes,
// 	})

// 	publicKeyBytes, err := x509.MarshalPKIXPublicKey(kp.Public)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to marshal public key: %w", err)
// 	}
// 	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
// 		Type:  "PUBLIC KEY",
// 		Bytes: publicKeyBytes,
// 	})

// 	return map[string][]byte{
// 		PrivateKeySecretKey: privateKeyPEM,
// 		PublicKeySecretKey:  publicKeyPEM,
// 	}, nil
// }
