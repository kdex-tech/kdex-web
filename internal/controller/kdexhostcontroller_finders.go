package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *KDexHostControllerReconciler) findHostControllersForPageBinding(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	pageBinding, ok := obj.(*kdexv1alpha1.KDexPageBinding)
	if !ok {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      pageBinding.Spec.HostRef.Name,
				Namespace: pageBinding.Namespace,
			},
		},
	}
}

func (r *KDexHostControllerReconciler) findHostControllersForScriptLibrary(
	ctx context.Context,
	scriptLibrary client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var hostControllerList kdexv1alpha1.KDexHostControllerList
	if err := r.List(ctx, &hostControllerList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", r.FocalHost),
		Namespace:     scriptLibrary.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexHostControllers for scriptLibrary", "scriptLibrary", scriptLibrary.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(hostControllerList.Items))
	for _, hostController := range hostControllerList.Items {
		if hostController.Spec.Host.ScriptLibraryRef == nil {
			continue
		}
		if hostController.Spec.Host.ScriptLibraryRef.Name == scriptLibrary.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      hostController.Name,
					Namespace: hostController.Namespace,
				},
			})
		}
	}
	return requests
}

func (r *KDexHostControllerReconciler) findHostControllersForTheme(
	ctx context.Context,
	theme client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var hostControllerList kdexv1alpha1.KDexHostControllerList
	if err := r.List(ctx, &hostControllerList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", r.FocalHost),
		Namespace:     theme.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexHostControllers for theme", "theme", theme.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(hostControllerList.Items))
	for _, hostController := range hostControllerList.Items {
		if hostController.Spec.Host.DefaultThemeRef == nil {
			continue
		}
		if hostController.Spec.Host.DefaultThemeRef.Name == theme.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      hostController.Name,
					Namespace: hostController.Namespace,
				},
			})
		}
	}
	return requests
}
