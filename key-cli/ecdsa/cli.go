package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/kdex-tech/kdex-host/internal/keys"
)

func main() {
	// 1. Generate the keypair using your existing function
	keyPairs := keys.GenerateECDSAKeyPair()

	// Since GenerateKeyPair returns a slice/struct (*KeyPairs),
	// we grab the first active one.
	activeKeyPair := (*keyPairs)[0]
	privateKey := activeKeyPair.Private

	// 2. Convert the ECDSA private key to PKCS#8 DER bytes
	derBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		fmt.Printf("Failed to marshal private key: %v\n", err)
		return
	}

	// 3. Wrap it in a PEM block
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: derBytes,
		Headers: map[string]string{
			"KID": activeKeyPair.KeyId,
		},
	}

	pemString := string(pem.EncodeToMemory(block))

	// 5. Print to console for easy copy-pasting into your test mock
	fmt.Println("--- COPY THIS RSA PRIVATE KEY INTO YOUR TEST ---")
	fmt.Println(pemString)
	fmt.Printf("KID for this key: %s\n", activeKeyPair.KeyId)
}
