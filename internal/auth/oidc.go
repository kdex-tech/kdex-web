package auth

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func LoadClientSecret(
	ctx context.Context,
	c client.Client,
	namespace string,
	secretRef *kdexv1alpha1.LocalSecretWithKeyReference,
) (string, error) {
	if secretRef == nil {
		return "", nil
	}

	var secret corev1.Secret
	err := c.Get(ctx, client.ObjectKey{
		Name:      secretRef.SecretRef.Name,
		Namespace: namespace,
	}, &secret)

	if err != nil {
		return "", err
	}

	clientSecret, ok := secret.Data[secretRef.KeyProperty]
	if !ok {
		return "", fmt.Errorf("secret %s/%s does not contain %s", namespace, secret.Name, secretRef.KeyProperty)
	}
	return string(clientSecret), nil
}
