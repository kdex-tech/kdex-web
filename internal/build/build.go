package build

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	"kdex.dev/web/internal"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Builder struct {
	client.Client
	Scheme        *runtime.Scheme
	Configuration configuration.NexusConfiguration
	Source        kdexv1alpha1.Source
}

func (b *Builder) GetOrCreateKPackImage(
	ctx context.Context,
	function *kdexv1alpha1.KDexFunction,
) (controllerutil.OperationResult, *unstructured.Unstructured, error) {

	kImageName := fmt.Sprintf("%s-%s", function.Spec.HostRef.Name, function.Name)

	kImage := &unstructured.Unstructured{}
	kImage.SetGroupVersionKind(internal.KPackImageGVK)
	kImage.SetNamespace(function.Namespace)
	kImage.SetName(kImageName)

	op, err := ctrl.CreateOrPatch(ctx, b.Client, kImage, func() error {
		spec := map[string]any{
			"build": map[string]any{
				"env": []any{
					map[string]any{
						"name":  "BP_LOG_LEVEL",
						"value": "DEBUG",
					},
				},
			},
			"builder": map[string]any{
				"name": "tiny-microservice-builder",
				"kind": "ClusterBuilder",
			},
			"imageTaggingStrategy": "BuildNumber",
			"source": map[string]any{
				"git": map[string]any{
					"url":      b.Source.Repository,
					"revision": b.Source.Revision,
				},
				"subPath": b.Source.Path,
			},
			"tag": fmt.Sprintf("%s/%s/%s:latest", b.Configuration.DefaultImageRegistry.Host, function.Spec.HostRef.Name, function.Name),
			"additionalTags": []any{
				fmt.Sprintf("%s/%s/%s:%d", b.Configuration.DefaultImageRegistry.Host, function.Spec.HostRef.Name, function.Name, function.GetGeneration()),
			},
		}

		unstructured.SetNestedMap(kImage.Object, spec, "spec")

		kImage.SetLabels(map[string]string{
			"app":           "builder",
			"function":      function.Name,
			"kdex.dev/host": function.Spec.HostRef.Name,
		})

		return ctrl.SetControllerReference(function, kImage, b.Scheme)
	})

	if err != nil {
		return op, kImage, fmt.Errorf("failed to create image builder: %w", err)
	}

	return op, kImage, nil
}
