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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const UTILITY_PAGE_FINALIZER = "kdex.dev/kdex-web-utility-page-finalizer"

// KDexInternalUtilityPageReconciler reconciles a KDexInternalUtilityPage object
type KDexInternalUtilityPageReconciler struct {
	client.Client
	ControllerNamespace string
	FocalHost           string
	HostHandler         *host.HostHandler
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalutilitypages,           verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalutilitypages/finalizers,verbs=update
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalutilitypages/status,    verbs=get;update;patch

// nolint:gocyclo
func (r *KDexInternalUtilityPageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	if req.Namespace != r.ControllerNamespace {
		log.V(1).Info("skipping reconcile", "namespace", req.Namespace, "controllerNamespace", r.ControllerNamespace)
		return ctrl.Result{}, nil
	}

	var internalUtilityPage kdexv1alpha1.KDexInternalUtilityPage
	if err := r.Get(ctx, req.NamespacedName, &internalUtilityPage); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Only process utility pages for the focal host
	if internalUtilityPage.Spec.HostRef.Name != r.FocalHost {
		log.V(1).Info("skipping utility page for non-focal host",
			"hostRef", internalUtilityPage.Spec.HostRef.Name,
			"focalHost", r.FocalHost)
		return ctrl.Result{}, nil
	}

	if internalUtilityPage.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&internalUtilityPage, UTILITY_PAGE_FINALIZER) {
			controllerutil.AddFinalizer(&internalUtilityPage, UTILITY_PAGE_FINALIZER)
			if err := r.Update(ctx, &internalUtilityPage); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&internalUtilityPage, UTILITY_PAGE_FINALIZER) {
			r.HostHandler.RemoveUtilityPage(internalUtilityPage.Name)

			controllerutil.RemoveFinalizer(&internalUtilityPage, UTILITY_PAGE_FINALIZER)
			if err := r.Update(ctx, &internalUtilityPage); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if internalUtilityPage.Status.Attributes == nil {
		internalUtilityPage.Status.Attributes = make(map[string]string)
	}

	// Defer status update
	defer func() {
		internalUtilityPage.Status.ObservedGeneration = internalUtilityPage.Generation
		if updateErr := r.Status().Update(ctx, &internalUtilityPage); updateErr != nil {
			err = updateErr
		}

		log.V(1).Info("status", "status", internalUtilityPage.Status, "err", err)
	}()

	kdexv1alpha1.SetConditions(
		&internalUtilityPage.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		"Reconciling",
	)

	archetypeObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalUtilityPage, &internalUtilityPage.Status.Conditions, &internalUtilityPage.Spec.PageArchetypeRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	internalUtilityPage.Status.Attributes["archetype.generation"] = fmt.Sprintf("%d", archetypeObj.GetGeneration())

	var pageArchetypeSpec kdexv1alpha1.KDexPageArchetypeSpec

	switch v := archetypeObj.(type) {
	case *kdexv1alpha1.KDexPageArchetype:
		pageArchetypeSpec = v.Spec
	case *kdexv1alpha1.KDexClusterPageArchetype:
		pageArchetypeSpec = v.Spec
	}

	contents, shouldReturn, response, err := ResolveContents(ctx, r.Client, &internalUtilityPage, &internalUtilityPage.Status.Conditions, internalUtilityPage.Spec.ContentEntries, r.RequeueDelay)
	if shouldReturn {
		return response, err
	}

	for k, content := range contents {
		if content.App != nil {
			internalUtilityPage.Status.Attributes[k+".content.generation"] = content.Content.AppGeneration
		}
	}

	headerRef := internalUtilityPage.Spec.OverrideHeaderRef
	if headerRef == nil {
		headerRef = pageArchetypeSpec.DefaultHeaderRef
	}
	headerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalUtilityPage, &internalUtilityPage.Status.Conditions, headerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if headerObj != nil {
		internalUtilityPage.Status.Attributes["header.generation"] = fmt.Sprintf("%d", headerObj.GetGeneration())
	}

	footerRef := internalUtilityPage.Spec.OverrideFooterRef
	if footerRef == nil {
		footerRef = pageArchetypeSpec.DefaultFooterRef
	}
	footerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalUtilityPage, &internalUtilityPage.Status.Conditions, footerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if footerObj != nil {
		internalUtilityPage.Status.Attributes["footer.generation"] = fmt.Sprintf("%d", footerObj.GetGeneration())
	}

	navigationRefs := pageArchetypeSpec.DefaultNavigationRefs
	if len(internalUtilityPage.Spec.OverrideNavigationRefs) > 0 {
		if navigationRefs == nil {
			navigationRefs = make(map[string]*kdexv1alpha1.KDexObjectReference)
		}
		maps.Copy(navigationRefs, internalUtilityPage.Spec.OverrideNavigationRefs)
	}
	navigations, shouldReturn, r1, err := ResolvePageNavigations(ctx, r.Client, &internalUtilityPage, &internalUtilityPage.Status.Conditions, navigationRefs, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	for k, navigation := range navigations {
		internalUtilityPage.Status.Attributes[k+".navigation.generation"] = fmt.Sprintf("%d", navigation.Generation)
	}

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalUtilityPage, &internalUtilityPage.Status.Conditions, internalUtilityPage.Spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		internalUtilityPage.Status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())
	}

	_, shouldReturn, r1, err = ResolveHost(ctx, r.Client, &internalUtilityPage, &internalUtilityPage.Status.Conditions, &internalUtilityPage.Spec.HostRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
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
	for _, navigation := range navigations {
		navigationsMap[navigation.Name] = navigation.Spec.Content
	}

	r.HostHandler.AddOrUpdateUtilityPage(page.PageHandler{
		Content:      contentsMap,
		Footer:       footerContent,
		Header:       headerContent,
		MainTemplate: pageArchetypeSpec.Content,
		Navigations:  navigationsMap,
		Name:         internalUtilityPage.Name,
		UtilityPage:  &internalUtilityPage.Spec.KDexUtilityPageSpec,
	})

	kdexv1alpha1.SetConditions(
		&internalUtilityPage.Status.Conditions,
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
func (r *KDexInternalUtilityPageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexInternalUtilityPage{}).
		Watches(
			&kdexv1alpha1.KDexApp{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.ContentEntries[*].AppRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterApp{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.ContentEntries[*].AppRef}")).
		Watches(
			&kdexv1alpha1.KDexInternalHost{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.HostRef}")).
		Watches(
			&kdexv1alpha1.KDexPageArchetype{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.PageArchetypeRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageArchetype{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.PageArchetypeRef}")).
		Watches(
			&kdexv1alpha1.KDexPageFooter{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.OverrideFooterRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageFooter{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.OverrideFooterRef}")).
		Watches(
			&kdexv1alpha1.KDexPageHeader{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.OverrideHeaderRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageHeader{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.OverrideHeaderRef}")).
		Watches(
			&kdexv1alpha1.KDexPageNavigation{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.OverrideNavigationRefs.*}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageNavigation{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.OverrideNavigationRefs.*}")).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.ScriptLibraryRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalUtilityPage{}, &kdexv1alpha1.KDexInternalUtilityPageList{}, "{.Spec.ScriptLibraryRef}")).
		WithOptions(
			controller.TypedOptions[reconcile.Request]{
				LogConstructor: LogConstructor("kdexinternalutilitypage", mgr),
			},
		).
		Named("kdexinternalutilitypage").
		Complete(r)
}
