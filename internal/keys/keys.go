package keys

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
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
	secrets []corev1.Secret,
	ttlSeconds int, // Unused? Kept for signature compatibility if needed, or remove.
	devMode bool,
) (*KeyPairs, error) {
	pairs := &KeyPairs{}
	found := false

	// Sort keys oldest to newest
	sort.Slice(secrets, func(i, j int) bool {
		return secrets[i].CreationTimestamp.Before(&secrets[j].CreationTimestamp)
	})

	// The newest secret with the active key annotation is the active key. If no
	// active key annotation is found, the newest secret is the active key.
	for _, secret := range secrets {
		isActive := false
		if secret.Annotations["kdex.dev/active-key"] == "true" {
			isActive = true
		}

		kp, err := LoadKeysFromSecret(&secret, isActive)
		if err != nil {
			return nil, err
		}

		found = true
		*pairs = append(*pairs, kp)
	}

	if found {
		if len(*pairs) > 1 && pairs.ActiveKey() == nil {
			// get a logger from context
			log := logf.FromContext(ctx)

			log.Info("Multiple keys exist but none are specified as the active key via annotation kdex.dev/active-key='true'. Defaulting to the newest key.")

			// set the newest key as active
			(*pairs)[len(*pairs)-1].ActiveKey = true
		}

		// If only one key, make it active if not already
		if len(*pairs) == 1 && pairs.ActiveKey() == nil {
			(*pairs)[0].ActiveKey = true
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

func GenerateRSAKeyPair() *KeyPairs {
	once.Do(func() {
		// 1. Generate a 2048-bit RSA key.
		// Note: RSA generation is mathematically more intensive than ECDSA
		// and may take a few hundred milliseconds.
		privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			// Panic because entropy is critical for RSA prime generation.
			panic(fmt.Errorf("failed to generate RSA key: %w", err))
		}

		// 2. Generate a unique ID based on the startup time.
		kid := fmt.Sprintf("kdex-rsa-dev-%d", time.Now().Unix())

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

// LoadKeyFromPEM loads a private key from a PEM encoded private key.
func LoadKeyFromPEM(privateKeyPEM []byte) (*KeyPair, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}

	var privKey any
	var err error

	// Try PKCS8 first (Modern standard, supports both RSA and ECDSA)
	privKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Fallback to PKCS1 (RSA Specific)
		privKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			// Fallback to SEC1 (EC Specific)
			privKey, err = x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse private key in any format: %w", err)
			}
		}
	}

	// Ensure it's a type that we support
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

	kid, ok := block.Headers["KID"]
	if ok {
		return &KeyPair{Private: signer, KeyId: kid}, nil
	}

	return &KeyPair{Private: signer}, nil
}

// LoadKeysFromSecret loads an RSA key pair from a Kubernetes Secret.
func LoadKeysFromSecret(secret *corev1.Secret, isActive bool) (*KeyPair, error) {
	privateKeyPEM, ok := secret.Data[PrivateKeySecretKey]
	if !ok {
		return nil, fmt.Errorf("secret does not contain %s", PrivateKeySecretKey)
	}

	key, err := LoadKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, err
	}

	if key.KeyId == "" {
		key.KeyId = secret.Name
	}

	key.ActiveKey = isActive

	return key, nil
}
