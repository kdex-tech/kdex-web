/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/store"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const hostFinalizerName = "kdex.dev/kdex-web-host-finalizer"

// KDexHostReconciler reconciles a KDexHost object
type KDexHostReconciler struct {
	client.Client
	HostStore    *store.HostStore
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhosts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhosts/finalizers,verbs=update
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexthemes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KDexHost object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KDexHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var host kdexv1alpha1.KDexHost
	if err := r.Get(ctx, req.NamespacedName, &host); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if host.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&host, hostFinalizerName) {
			controllerutil.AddFinalizer(&host, hostFinalizerName)
			if err := r.Update(ctx, &host); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&host, hostFinalizerName) {
			r.HostStore.Delete(host.Name)
			controllerutil.RemoveFinalizer(&host, hostFinalizerName)
			if err := r.Update(ctx, &host); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	stylesheet, shouldReturn, r1, err := resolveTheme(ctx, r.Client, &host, &host.Status.Conditions, host.Spec.DefaultThemeRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	log.Info("reconciled KDexHost")

	hostHandler := r.HostStore.GetOrDefault(
		host.Name, stylesheet, log.WithName("host-handler").WithValues("host", host.Name))
	hostHandler.SetHost(&host, stylesheet)

	apimeta.SetStatusCondition(
		&host.Status.Conditions,
		*kdexv1alpha1.NewCondition(
			kdexv1alpha1.ConditionTypeReady,
			metav1.ConditionTrue,
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			"all references resolved successfully",
		),
	)

	if err := r.Status().Update(ctx, &host); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexHost{}).
		Watches(
			&kdexv1alpha1.KDexTheme{},
			handler.EnqueueRequestsFromMapFunc(r.findHostsForTheme)).
		Named("kdexhost").
		Complete(r)
}

func (r *KDexHostReconciler) findHostsForTheme(
	ctx context.Context,
	stylesheet client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var hostsList kdexv1alpha1.KDexHostList
	if err := r.List(ctx, &hostsList, &client.ListOptions{
		Namespace: stylesheet.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexHost for stylesheet", "name", stylesheet.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(hostsList.Items))
	for _, host := range hostsList.Items {
		if host.Spec.DefaultThemeRef == nil {
			continue
		}
		if host.Spec.DefaultThemeRef.Name == stylesheet.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      host.Name,
					Namespace: host.Namespace,
				},
			})
		}
	}
	return requests
}
