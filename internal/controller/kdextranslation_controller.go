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

const translationFinalizerName = "kdex.dev/kdex-web-translation-finalizer"

// KDexTranslationReconciler reconciles a KDexTranslation object
type KDexTranslationReconciler struct {
	client.Client
	HostStore    *store.HostStore
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhost,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdextranslations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdextranslations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdextranslations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KDexTranslation object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *KDexTranslationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var translation kdexv1alpha1.KDexTranslation
	if err := r.Get(ctx, req.NamespacedName, &translation); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if translation.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&translation, translationFinalizerName) {
			controllerutil.AddFinalizer(&translation, translationFinalizerName)
			if err := r.Update(ctx, &translation); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&translation, translationFinalizerName) {
			hostHandler, ok := r.HostStore.Get(translation.Spec.HostRef.Name)

			if ok {
				hostHandler.RemoveTranslation(translation)
			}

			controllerutil.RemoveFinalizer(&translation, translationFinalizerName)
			if err := r.Update(ctx, &translation); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	_, shouldReturn, r1, err := resolveHost(ctx, r.Client, &translation, &translation.Status.Conditions, &translation.Spec.HostRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	log.Info("reconciled KDexTranslation")

	hostHandler, ok := r.HostStore.Get(translation.Spec.HostRef.Name)

	if !ok {
		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	hostHandler.AddOrUpdateTranslation(translation)

	apimeta.SetStatusCondition(
		&translation.Status.Conditions,
		*kdexv1alpha1.NewCondition(
			kdexv1alpha1.ConditionTypeReady,
			metav1.ConditionTrue,
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			"all references resolved successfully",
		),
	)

	if err := r.Status().Update(ctx, &translation); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexTranslationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexTranslation{}).
		Watches(
			&kdexv1alpha1.KDexHost{},
			handler.EnqueueRequestsFromMapFunc(r.findTranslationsForHost)).
		Named("kdextranslation").
		Complete(r)
}

func (r *KDexTranslationReconciler) findTranslationsForHost(
	ctx context.Context,
	host client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var translationList kdexv1alpha1.KDexTranslationList
	if err := r.List(ctx, &translationList, &client.ListOptions{
		Namespace: host.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexTranslation for host", "name", host.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(translationList.Items))
	for _, translation := range translationList.Items {
		if translation.Spec.HostRef.Name == host.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      translation.Name,
					Namespace: translation.Namespace,
				},
			})
		}
	}
	return requests
}
