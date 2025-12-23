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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/host"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const translationFinalizerName = "kdex.dev/kdex-web-translation-finalizer"

// KDexInternalTranslationReconciler reconciles a KDexInternalTranslation object
type KDexInternalTranslationReconciler struct {
	client.Client
	ControllerNamespace string
	FocalHost           string
	HostStore           *host.HostStore
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternaltranslations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternaltranslations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternaltranslations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KDexInternalTranslation object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *KDexInternalTranslationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	if req.Namespace != r.ControllerNamespace {
		log.V(1).Info("skipping reconcile", "namespace", req.Namespace, "controllerNamespace", r.ControllerNamespace)
		return ctrl.Result{}, nil
	}

	if req.Name != r.FocalHost {
		log.V(1).Info("skipping reconcile", "host", req.Name, "focalHost", r.FocalHost)
		return ctrl.Result{}, nil
	}

	var translation kdexv1alpha1.KDexInternalTranslation
	if err := r.Get(ctx, req.NamespacedName, &translation); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Defer status update
	defer func() {
		translation.Status.ObservedGeneration = translation.Generation
		if updateErr := r.Status().Update(ctx, &translation); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}

		log.V(1).Info("status", "status", translation.Status, "err", err, "res", res)
	}()

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
				hostHandler.RemoveTranslation(translation.Name)
			}

			controllerutil.RemoveFinalizer(&translation, translationFinalizerName)
			if err := r.Update(ctx, &translation); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	kdexv1alpha1.SetConditions(
		&translation.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		"Reconciling",
	)

	hostHandler, ok := r.HostStore.Get(translation.Spec.HostRef.Name)

	if !ok {
		kdexv1alpha1.SetConditions(
			&translation.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			"Host not found",
		)

		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	hostHandler.AddOrUpdateTranslation(translation.Name, &translation.Spec)

	kdexv1alpha1.SetConditions(
		&translation.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	log.V(1).Info("reconciled")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexInternalTranslationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasFocalHost := func(o client.Object) bool {
		switch t := o.(type) {
		case *kdexv1alpha1.KDexHostController:
			return t.Name == r.FocalHost
		case *kdexv1alpha1.KDexHostPackageReferences:
			return t.Name == fmt.Sprintf("%s-packages", r.FocalHost)
		case *kdexv1alpha1.KDexPageBinding:
			return t.Spec.HostRef.Name == r.FocalHost
		case *kdexv1alpha1.KDexInternalTranslation:
			return t.Spec.HostRef.Name == r.FocalHost
		default:
			return true
		}
	}

	var enabledFilter = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasFocalHost(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasFocalHost(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasFocalHost(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasFocalHost(e.Object)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexInternalTranslation{}).
		WithEventFilter(enabledFilter).
		WithOptions(
			controller.TypedOptions[reconcile.Request]{
				LogConstructor: LogConstructor("kdexinternaltranslation", mgr),
			},
		).
		Named("kdexinternaltranslation").
		Complete(r)
}
