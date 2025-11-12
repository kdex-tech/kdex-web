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
	"kdex.dev/web/internal/store"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const pageBindingFinalizerName = "kdex.dev/kdex-nexus-page-binding-finalizer"

// KDexPageBindingReconciler reconciles a KDexPageBinding object
type KDexPageBindingReconciler struct {
	client.Client
	HostStore    *store.HostStore
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexapps,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhosts,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagearchetypes,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagebindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagebindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagefooters,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpageheaders,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagenavigations,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexscriptlibraries,verbs=get;list;watch

func (r *KDexPageBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var pageBinding kdexv1alpha1.KDexPageBinding
	if err := r.Get(ctx, req.NamespacedName, &pageBinding); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if pageBinding.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&pageBinding, pageBindingFinalizerName) {
			controllerutil.AddFinalizer(&pageBinding, pageBindingFinalizerName)
			if err := r.Update(ctx, &pageBinding); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&pageBinding, pageBindingFinalizerName) {
			hostHandler, ok := r.HostStore.Get(pageBinding.Spec.HostRef.Name)
			if ok {
				hostHandler.Pages.Delete(pageBinding.Name)
			}

			controllerutil.RemoveFinalizer(&pageBinding, pageBindingFinalizerName)
			if err := r.Update(ctx, &pageBinding); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	kdexv1alpha1.SetConditions(
		&pageBinding.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		"Reconciling",
	)
	if err := r.Status().Update(ctx, &pageBinding); err != nil {
		return ctrl.Result{}, err
	}

	// Defer status update
	defer func() {
		pageBinding.Status.ObservedGeneration = pageBinding.Generation
		if err := r.Status().Update(ctx, &pageBinding); err != nil {
			log.Info("failed to update status", "err", err)
		}
	}()

	return r.innerReconcile(ctx, &pageBinding)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexPageBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageBinding{}).
		Watches(
			&kdexv1alpha1.KDexApp{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForApp)).
		Watches(
			&kdexv1alpha1.KDexHost{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForHost)).
		Watches(
			&kdexv1alpha1.KDexPageArchetype{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForPageArchetype)).
		Watches(
			&kdexv1alpha1.KDexPageBinding{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForPageBindings)).
		Watches(
			&kdexv1alpha1.KDexPageFooter{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForPageFooter)).
		Watches(
			&kdexv1alpha1.KDexPageHeader{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForPageHeader)).
		Watches(
			&kdexv1alpha1.KDexPageNavigation{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForPageNavigations)).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			handler.EnqueueRequestsFromMapFunc(r.findPageBindingsForScriptLibrary)).
		Named("kdexpagebinding").
		Complete(r)
}

//nolint:gocyclo
func (r *KDexPageBindingReconciler) innerReconcile(
	ctx context.Context, pageBinding *kdexv1alpha1.KDexPageBinding,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	scriptLibraries := []kdexv1alpha1.KDexScriptLibrary{}

	host, shouldReturn, r1, err := resolveHost(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, &pageBinding.Spec.HostRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	hostScriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, host.Spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if hostScriptLibrary != nil {
		scriptLibraries = append(scriptLibraries, *hostScriptLibrary)
	}

	archetype, shouldReturn, r1, err := resolvePageArchetype(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, &pageBinding.Spec.PageArchetypeRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	archetypeScriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, archetype.Spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if archetypeScriptLibrary != nil {
		scriptLibraries = append(scriptLibraries, *archetypeScriptLibrary)
	}

	contents, shouldReturn, response, err := resolveContents(ctx, r.Client, pageBinding, r.RequeueDelay)
	if shouldReturn {
		return response, err
	}

	navigationRef := pageBinding.Spec.OverrideMainNavigationRef
	if navigationRef == nil {
		navigationRef = archetype.Spec.DefaultMainNavigationRef
	}
	navigations, shouldReturn, response, err := resolvePageNavigations(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, navigationRef, archetype.Spec.ExtraNavigations, r.RequeueDelay)
	if shouldReturn {
		return response, err
	}

	for _, navigation := range navigations {
		navigationScriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, navigation.Spec.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if navigationScriptLibrary != nil {
			scriptLibraries = append(scriptLibraries, *navigationScriptLibrary)
		}
	}

	_, shouldReturn, r1, err = resolvePageBinding(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageBinding.Spec.ParentPageRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	headerRef := pageBinding.Spec.OverrideHeaderRef
	if headerRef == nil {
		headerRef = archetype.Spec.DefaultHeaderRef
	}
	header, shouldReturn, r1, err := resolvePageHeader(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, headerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if header != nil {
		headerScriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, header.Spec.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if headerScriptLibrary != nil {
			scriptLibraries = append(scriptLibraries, *headerScriptLibrary)
		}
	}

	footerRef := pageBinding.Spec.OverrideFooterRef
	if footerRef == nil {
		footerRef = archetype.Spec.DefaultFooterRef
	}
	footer, shouldReturn, r1, err := resolvePageFooter(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, footerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if footer != nil {
		footerScriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, footer.Spec.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if footerScriptLibrary != nil {
			scriptLibraries = append(scriptLibraries, *footerScriptLibrary)
		}
	}

	pageBindingScriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageBinding.Spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if pageBindingScriptLibrary != nil {
		scriptLibraries = append(scriptLibraries, *pageBindingScriptLibrary)
	}

	if pageBinding.Spec.BasePath == "/" && pageBinding.Spec.ParentPageRef != nil {
		err := fmt.Errorf("a page binding with basePath set to '/' must not specify a parent page binding")

		kdexv1alpha1.SetConditions(
			&pageBinding.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)
		if err := r.Status().Update(ctx, pageBinding); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	hostHandler, ok := r.HostStore.Get(pageBinding.Spec.HostRef.Name)

	if !ok {
		kdexv1alpha1.SetConditions(
			&pageBinding.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			"Host not found",
		)
		if err := r.Status().Update(ctx, pageBinding); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	hostHandler.Pages.Set(store.PageHandler{
		Archetype:       archetype,
		Content:         contents,
		Footer:          footer,
		Header:          header,
		Navigations:     navigations,
		Page:            pageBinding,
		ScriptLibraries: scriptLibraries,
	})

	kdexv1alpha1.SetConditions(
		&pageBinding.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)
	if err := r.Status().Update(ctx, pageBinding); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciled KDexPageBinding")

	return ctrl.Result{}, nil
}
