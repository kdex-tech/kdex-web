package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"kdex.dev/web/internal/auth"
)

func main() {
	// 1. Generate the RSA keypair using your function
	keys := GenerateRSAKeyPair()
	activeKeyPair := (*keys)[0]

	// 2. Assert the key is RSA (since Private is a crypto.Signer interface)
	rsaPriv, ok := activeKeyPair.Private.(*rsa.PrivateKey)
	if !ok {
		panic("Key is not an RSA private key")
	}

	// 3. Convert to PKCS#1 DER bytes (Standard for RSA)
	// This creates the "BEGIN RSA PRIVATE KEY" format
	derBytes := x509.MarshalPKCS1PrivateKey(rsaPriv)

	// 4. Encode to PEM string
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: derBytes,
	}

	pemString := string(pem.EncodeToMemory(block))

	// 5. Print to console for easy copy-pasting into your test mock
	fmt.Println("--- COPY THIS ECDSA PRIVATE KEY INTO YOUR TEST ---")
	fmt.Println(pemString)
	fmt.Printf("KID for this key: %s\n", activeKeyPair.KeyId)
}

var (
	instance *auth.KeyPairs
	once     sync.Once
)

func GenerateRSAKeyPair() *auth.KeyPairs {
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

		instance = &auth.KeyPairs{
			{
				ActiveKey: true,
				KeyId:     kid,
				Private:   privateKey, // *rsa.PrivateKey implements crypto.Signer
			},
		}
	})
	return instance
}
