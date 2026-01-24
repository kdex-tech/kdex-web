package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// PrivateKeySecretKey is the key in the secret that contains the private key
	PrivateKeySecretKey = "private-key"
	// PublicKeySecretKey is the key in the secret that contains the public key
	PublicKeySecretKey = "public-key"
)

// KeyPair holds an RSA key pair for JWT signing and verification.
type KeyPair struct {
	Private *rsa.PrivateKey
	Public  *rsa.PublicKey
}

// LoadOrGenerateKeyPair loads an RSA key pair from a Kubernetes Secret.
// If the secret doesn't exist or is invalid, it generates a new key pair.
func LoadOrGenerateKeyPair(ctx context.Context, c client.Client, secretRef types.NamespacedName) (*KeyPair, error) {
	var secret corev1.Secret
	err := c.Get(ctx, secretRef, &secret)

	if err == nil {
		// Try to load keys from secret
		kp, loadErr := loadKeysFromSecret(&secret)
		if loadErr == nil {
			return kp, nil
		}
		// If loading fails, fall through to generate new keys
		logf.Log.WithName("auth-keys").Info("Failed to load keys from secret, generating new ones", "error", loadErr)
	}

	// Generate new key pair
	return GenerateKeyPair()
}

// GenerateKeyPair generates a new RSA key pair for JWT signing.
func GenerateKeyPair() (*KeyPair, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	return &KeyPair{
		Private: privateKey,
		Public:  &privateKey.PublicKey,
	}, nil
}

// loadKeysFromSecret loads an RSA key pair from a Kubernetes Secret.
func loadKeysFromSecret(secret *corev1.Secret) (*KeyPair, error) {
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
		Private: privateKey,
		Public:  &privateKey.PublicKey,
	}, nil
}

// ExportToSecret exports the key pair to a format suitable for storing in a Kubernetes Secret.
func (kp *KeyPair) ExportToSecret() (map[string][]byte, error) {
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(kp.Private)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(kp.Public)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return map[string][]byte{
		PrivateKeySecretKey: privateKeyPEM,
		PublicKeySecretKey:  publicKeyPEM,
	}, nil
}
