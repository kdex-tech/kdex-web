package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *KDexInternalHostReconciler) findInternalHostsForPageBinding(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	pageBinding, ok := obj.(*kdexv1alpha1.KDexPageBinding)
	if !ok {
		return nil
	}

	if pageBinding.Spec.HostRef.Name != r.FocalHost {
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
