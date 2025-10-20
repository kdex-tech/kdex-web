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

	"k8s.io/apimachinery/pkg/api/errors"
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

const renderPageFinalizerName = "kdex.dev/kdex-web-render-page-finalizer"

// MicroFrontEndRenderPageReconciler reconciles a MicroFrontEndRenderPage object
type MicroFrontEndRenderPageReconciler struct {
	client.Client
	HostStore    *store.HostStore
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=microfrontendhost,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=microfrontendrenderpages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=microfrontendrenderpages/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=microfrontendrenderpages/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MicroFrontEndRenderPage object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *MicroFrontEndRenderPageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var renderPage kdexv1alpha1.MicroFrontEndRenderPage
	if err := r.Get(ctx, req.NamespacedName, &renderPage); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if renderPage.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&renderPage, renderPageFinalizerName) {
			controllerutil.AddFinalizer(&renderPage, renderPageFinalizerName)
			if err := r.Update(ctx, &renderPage); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&renderPage, renderPageFinalizerName) {
			trackedHost, ok := r.HostStore.Get(renderPage.Spec.HostRef.Name)
			if ok {
				trackedHost.RenderPages.Delete(renderPage.Name)
			}
			controllerutil.RemoveFinalizer(&renderPage, renderPageFinalizerName)
			if err := r.Update(ctx, &renderPage); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	var host kdexv1alpha1.MicroFrontEndHost
	hostName := types.NamespacedName{
		Name:      renderPage.Spec.HostRef.Name,
		Namespace: renderPage.Namespace,
	}
	if err := r.Get(ctx, hostName, &host); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "referenced MicroFrontEndHost not found", "name", renderPage.Spec.HostRef.Name)
			apimeta.SetStatusCondition(
				&renderPage.Status.Conditions,
				*kdexv1alpha1.NewCondition(
					kdexv1alpha1.ConditionTypeReady,
					metav1.ConditionFalse,
					kdexv1alpha1.ConditionReasonReconcileError,
					fmt.Sprintf("referenced MicroFrontEndHost %s not found", renderPage.Spec.HostRef.Name),
				),
			)
			if err := r.Status().Update(ctx, &renderPage); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
		}

		log.Error(err, "unable to fetch MicroFrontEndHost", "name", renderPage.Spec.HostRef.Name)
		return ctrl.Result{}, err
	}

	var stylesheet kdexv1alpha1.MicroFrontEndStylesheet
	if renderPage.Spec.StylesheetRef != nil {
		stylesheetName := types.NamespacedName{
			Name:      renderPage.Spec.StylesheetRef.Name,
			Namespace: renderPage.Namespace,
		}
		if err := r.Get(ctx, stylesheetName, &stylesheet); err != nil {
			if errors.IsNotFound(err) {
				log.Error(err, "referenced MicroFrontEndStylesheet not found", "name", stylesheetName.Name)
				apimeta.SetStatusCondition(
					&host.Status.Conditions,
					*kdexv1alpha1.NewCondition(
						kdexv1alpha1.ConditionTypeReady,
						metav1.ConditionFalse,
						kdexv1alpha1.ConditionReasonReconcileError,
						fmt.Sprintf("referenced MicroFrontEndStylesheet %s not found", stylesheetName.Name),
					),
				)
				if err := r.Status().Update(ctx, &renderPage); err != nil {
					return ctrl.Result{}, err
				}

				return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
			}

			log.Error(err, "unable to fetch MicroFrontEndStylesheet", "name", stylesheetName.Name)
			return ctrl.Result{}, err
		}
	}

	trackedHost, ok := r.HostStore.Get(renderPage.Spec.HostRef.Name)

	if !ok {
		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	trackedHost.RenderPages.Set(store.RenderPageHandler{
		Page:       renderPage,
		Stylesheet: &stylesheet,
	})

	log.Info("reconciled MicroFrontEndRenderPage")

	apimeta.SetStatusCondition(
		&renderPage.Status.Conditions,
		*kdexv1alpha1.NewCondition(
			kdexv1alpha1.ConditionTypeReady,
			metav1.ConditionTrue,
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			"all references resolved successfully",
		),
	)

	if err := r.Status().Update(ctx, &renderPage); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MicroFrontEndRenderPageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.MicroFrontEndRenderPage{}).
		Watches(
			&kdexv1alpha1.MicroFrontEndHost{},
			handler.EnqueueRequestsFromMapFunc(r.findRenderPagesForHost)).
		Named("microfrontendrenderpage").
		Complete(r)
}

func (r *MicroFrontEndRenderPageReconciler) findRenderPagesForHost(
	ctx context.Context,
	host client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var renderPageList kdexv1alpha1.MicroFrontEndRenderPageList
	if err := r.List(ctx, &renderPageList, &client.ListOptions{
		Namespace: host.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list MicroFrontEndRenderPage for host", "name", host.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(renderPageList.Items))
	for _, renderPage := range renderPageList.Items {
		if renderPage.Spec.HostRef.Name == host.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      renderPage.Name,
					Namespace: renderPage.Namespace,
				},
			})
		}
	}
	return requests
}
