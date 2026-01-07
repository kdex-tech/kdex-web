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
	"maps"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/host"
	"kdex.dev/web/internal/page"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KDexInternalPageBindingReconciler reconciles a KDexInternalPageBinding object
type KDexInternalPageBindingReconciler struct {
	client.Client
	ControllerNamespace string
	FocalHost           string
	HostHandler         *host.HostHandler
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
}

func (r *KDexInternalPageBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	if req.Namespace != r.ControllerNamespace {
		log.V(1).Info("skipping reconcile", "namespace", req.Namespace, "controllerNamespace", r.ControllerNamespace)
		return ctrl.Result{}, nil
	}

	var pageBinding kdexv1alpha1.KDexInternalPageBinding
	if err := r.Get(ctx, req.NamespacedName, &pageBinding); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if pageBinding.Spec.HostRef.Name != r.FocalHost {
		log.V(1).Info("skipping reconcile", "host", pageBinding.Spec.HostRef.Name, "focalHost", r.FocalHost)
		return ctrl.Result{}, nil
	}

	// Defer status update
	defer func() {
		pageBinding.Status.ObservedGeneration = pageBinding.Generation
		if updateErr := r.Status().Update(ctx, &pageBinding); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}

		log.V(1).Info("status", "status", pageBinding.Status, "err", err, "res", res)
	}()

	if pageBinding.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&pageBinding, PAGE_BINDING_FINALIZER) {
			controllerutil.AddFinalizer(&pageBinding, PAGE_BINDING_FINALIZER)
			if err := r.Update(ctx, &pageBinding); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&pageBinding, PAGE_BINDING_FINALIZER) {
			r.HostHandler.Pages.Delete(pageBinding.Name)

			controllerutil.RemoveFinalizer(&pageBinding, PAGE_BINDING_FINALIZER)
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

	return r.innerReconcile(ctx, &pageBinding)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexInternalPageBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	l := LogConstructor("kdexinternalpagebinding", mgr)(nil)

	hasFocalHost := func(o client.Object) bool {
		l.V(3).Info("hasFocalHost", "object", o)
		switch t := o.(type) {
		case *kdexv1alpha1.KDexInternalHost:
			return t.Name == r.FocalHost
		case *kdexv1alpha1.KDexInternalPackageReferences:
			return t.Name == fmt.Sprintf("%s-packages", r.FocalHost)
		case *kdexv1alpha1.KDexInternalPageBinding:
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
		For(&kdexv1alpha1.KDexInternalPageBinding{}).
		Watches(
			&kdexv1alpha1.KDexApp{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.ContentEntries[*].AppRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterApp{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.ContentEntries[*].AppRef}")).
		Watches(
			&kdexv1alpha1.KDexInternalHost{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.HostRef}")).
		Watches(
			&kdexv1alpha1.KDexPageArchetype{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.PageArchetypeRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageArchetype{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.PageArchetypeRef}")).
		Watches(
			&kdexv1alpha1.KDexInternalPageBinding{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.ParentPageRef}")).
		Watches(
			&kdexv1alpha1.KDexPageFooter{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.OverrideFooterRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageFooter{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.OverrideFooterRef}")).
		Watches(
			&kdexv1alpha1.KDexPageHeader{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.OverrideHeaderRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageHeader{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.OverrideHeaderRef}")).
		Watches(
			&kdexv1alpha1.KDexPageNavigation{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.OverrideNavigationRefs.*}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageNavigation{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.OverrideNavigationRefs.*}")).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.ScriptLibraryRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalPageBinding{}, &kdexv1alpha1.KDexInternalPageBindingList{}, "{.Spec.ScriptLibraryRef}")).
		WithEventFilter(enabledFilter).
		WithOptions(
			controller.TypedOptions[reconcile.Request]{
				LogConstructor: LogConstructor("kdexinternalpagebinding", mgr),
			},
		).
		Named("kdexinternalpagebinding").
		Complete(r)
}

//nolint:gocyclo
func (r *KDexInternalPageBindingReconciler) innerReconcile(
	ctx context.Context, pageBinding *kdexv1alpha1.KDexInternalPageBinding,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if pageBinding.Status.Attributes == nil {
		pageBinding.Status.Attributes = make(map[string]string)
	}

	archetypeObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, &pageBinding.Spec.PageArchetypeRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	pageBinding.Status.Attributes["archetype.generation"] = fmt.Sprintf("%d", archetypeObj.GetGeneration())

	var pageArchetypeSpec kdexv1alpha1.KDexPageArchetypeSpec

	switch v := archetypeObj.(type) {
	case *kdexv1alpha1.KDexPageArchetype:
		pageArchetypeSpec = v.Spec
	case *kdexv1alpha1.KDexClusterPageArchetype:
		pageArchetypeSpec = v.Spec
	}

	contents, shouldReturn, response, err := ResolveContents(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageBinding.Spec.ContentEntries, r.RequeueDelay)
	if shouldReturn {
		return response, err
	}

	for k, content := range contents {
		if content.App != nil {
			pageBinding.Status.Attributes[k+".content.generation"] = content.Content.AppGeneration
		}
	}

	headerRef := pageBinding.Spec.OverrideHeaderRef
	if headerRef == nil {
		headerRef = pageArchetypeSpec.DefaultHeaderRef
	}
	headerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, headerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if headerObj != nil {
		pageBinding.Status.Attributes["header.generation"] = fmt.Sprintf("%d", headerObj.GetGeneration())
	}

	footerRef := pageBinding.Spec.OverrideFooterRef
	if footerRef == nil {
		footerRef = pageArchetypeSpec.DefaultFooterRef
	}
	footerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, footerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if footerObj != nil {
		pageBinding.Status.Attributes["footer.generation"] = fmt.Sprintf("%d", footerObj.GetGeneration())
	}

	navigationRefs := maps.Clone(pageArchetypeSpec.DefaultNavigationRefs)
	if len(pageBinding.Spec.OverrideNavigationRefs) > 0 {
		if navigationRefs == nil {
			navigationRefs = make(map[string]*kdexv1alpha1.KDexObjectReference)
		}
		maps.Copy(navigationRefs, pageBinding.Spec.OverrideNavigationRefs)
	}
	navigations, shouldReturn, r1, err := ResolvePageNavigations(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, navigationRefs, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	for k, navigation := range navigations {
		pageBinding.Status.Attributes[k+".navigation.generation"] = fmt.Sprintf("%d", navigation.Generation)
	}

	parentPageObj, shouldReturn, r1, err := ResolvePageBinding(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageBinding.Spec.ParentPageRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if parentPageObj != nil {
		pageBinding.Status.Attributes["parent.pageBinding.generation"] = fmt.Sprintf("%d", parentPageObj.GetGeneration())
	}

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageBinding.Spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		pageBinding.Status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())
	}

	contentsMap := map[string]page.PackedContent{}
	for slot, content := range contents {
		contentsMap[slot] = content.Content
	}

	footerContent := ""
	if footerObj != nil {
		switch v := footerObj.(type) {
		case *kdexv1alpha1.KDexPageFooter:
			footerContent = v.Spec.Content
		case *kdexv1alpha1.KDexClusterPageFooter:
			footerContent = v.Spec.Content
		}
	}

	headerContent := ""
	if headerObj != nil {
		switch v := headerObj.(type) {
		case *kdexv1alpha1.KDexPageHeader:
			headerContent = v.Spec.Content
		case *kdexv1alpha1.KDexClusterPageHeader:
			headerContent = v.Spec.Content
		}
	}

	navigationsMap := map[string]string{}
	for slot, navigation := range navigations {
		navigationsMap[slot] = navigation.Spec.Content
	}

	r.HostHandler.Pages.Set(page.PageHandler{
		Content:           contentsMap,
		Footer:            footerContent,
		Header:            headerContent,
		MainTemplate:      pageArchetypeSpec.Content,
		Navigations:       navigationsMap,
		PackageReferences: pageBinding.Spec.PackageReferences,
		Name:              pageBinding.Name,
		Page:              &pageBinding.Spec.KDexPageBindingSpec,
		Scripts:           pageBinding.Spec.Scripts,
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

	log.V(1).Info("reconciled")

	return ctrl.Result{}, nil
}
