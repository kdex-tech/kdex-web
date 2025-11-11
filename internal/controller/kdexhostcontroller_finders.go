package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *KDexHostControllerReconciler) findHostControllersForHost(
	ctx context.Context,
	host client.Object,
) []reconcile.Request {
	if r.FocalHost != host.GetName() {
		return []reconcile.Request{}
	}

	log := logf.FromContext(ctx)

	var hostControllerList kdexv1alpha1.KDexHostControllerList
	if err := r.List(ctx, &hostControllerList, &client.ListOptions{
		Namespace: host.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexHostControllers for host", "host", host.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(hostControllerList.Items))
	for _, hostController := range hostControllerList.Items {
		if hostController.Spec.HostRef.Name == host.GetName() {
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
