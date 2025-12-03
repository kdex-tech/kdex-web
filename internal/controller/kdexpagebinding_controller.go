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
	"kdex.dev/web/internal/page"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const pageBindingFinalizerName = "kdex.dev/kdex-nexus-page-binding-finalizer"

// KDexPageBindingReconciler reconciles a KDexPageBinding object
type KDexPageBindingReconciler struct {
	client.Client
	ControllerNamespace string
	FocalHost           string
	HostStore           *host.HostStore
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexapps,                   verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterapps,            verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagearchetypes,         verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterpagearchetypes,  verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagebindings,           verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagebindings/status,    verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagebindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagefooters,            verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterpagefooters,     verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpageheaders,            verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterpageheaders,     verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagenavigations,        verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterpagenavigations, verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexscriptlibraries,        verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterscriptlibraries, verbs=get;list;watch

func (r *KDexPageBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var pageBinding kdexv1alpha1.KDexPageBinding
	if err := r.Get(ctx, req.NamespacedName, &pageBinding); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log := logf.Log.WithName("KDexPageBindingReconciler").WithValues("name", req.Name, "namespace", req.Namespace)

	// Defer status update
	defer func() {
		pageBinding.Status.ObservedGeneration = pageBinding.Generation
		if updateErr := r.Status().Update(ctx, &pageBinding); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}
		log.Info("print status", "status", pageBinding.Status, "err", err, "res", res)
	}()

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

	return r.innerReconcile(ctx, &pageBinding)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexPageBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageBinding{}).
		Watches(
			&kdexv1alpha1.KDexApp{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.ContentEntries[*].AppRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterApp{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.ContentEntries[*].AppRef}")).
		Watches(
			&kdexv1alpha1.KDexPageArchetype{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.PageArchetypeRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageArchetype{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.PageArchetypeRef}")).
		Watches(
			&kdexv1alpha1.KDexPageBinding{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.ParentPageRef}")).
		Watches(
			&kdexv1alpha1.KDexPageFooter{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.OverrideFooterRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageFooter{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.OverrideFooterRef}")).
		Watches(
			&kdexv1alpha1.KDexPageHeader{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.OverrideHeaderRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageHeader{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.OverrideHeaderRef}")).
		Watches(
			&kdexv1alpha1.KDexPageNavigation{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.OverrideMainNavigationRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterPageNavigation{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.OverrideMainNavigationRef}")).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.ScriptLibraryRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}, "{.Spec.ScriptLibraryRef}")).
		Named("kdexpagebinding").
		Complete(r)
}

//nolint:gocyclo
func (r *KDexPageBindingReconciler) innerReconcile(
	ctx context.Context, pageBinding *kdexv1alpha1.KDexPageBinding,
) (ctrl.Result, error) {
	log := logf.Log.WithName("KDexPageBindingReconciler").WithValues("name", pageBinding.Name, "namespace", pageBinding.Namespace)

	if pageBinding.Status.Attributes == nil {
		pageBinding.Status.Attributes = make(map[string]string)
	}

	scriptLibraries := []kdexv1alpha1.KDexScriptLibrarySpec{}

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

	archetypeScriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageArchetypeSpec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if archetypeScriptLibraryObj != nil {
		pageBinding.Status.Attributes["archetype.scriptLibrary.generation"] = fmt.Sprintf("%d", archetypeScriptLibraryObj.GetGeneration())

		var scriptLibrary kdexv1alpha1.KDexScriptLibrarySpec

		switch v := archetypeScriptLibraryObj.(type) {
		case *kdexv1alpha1.KDexScriptLibrary:
			scriptLibrary = v.Spec
		case *kdexv1alpha1.KDexClusterScriptLibrary:
			scriptLibrary = v.Spec
		}

		scriptLibraries = append(scriptLibraries, scriptLibrary)
	}

	contents, shouldReturn, response, err := ResolveContents(ctx, r.Client, pageBinding, r.RequeueDelay)
	if shouldReturn {
		return response, err
	}

	for k, content := range contents {
		if content.App != nil {
			pageBinding.Status.Attributes[k+".content.generation"] = content.AppGeneration
		}
	}

	navigationRef := pageBinding.Spec.OverrideMainNavigationRef
	if navigationRef == nil {
		navigationRef = pageArchetypeSpec.DefaultMainNavigationRef
	}
	navigations, shouldReturn, response, err := ResolvePageNavigations(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, navigationRef, pageArchetypeSpec.ExtraNavigations, r.RequeueDelay)
	if shouldReturn {
		return response, err
	}

	for k, navigation := range navigations {
		pageBinding.Status.Attributes[k+".navigation.generation"] = fmt.Sprintf("%d", navigation.Generation)

		navigationScriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, navigation.Spec.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if navigationScriptLibraryObj != nil {
			pageBinding.Status.Attributes[k+".navigation.scriptLibrary.generation"] = fmt.Sprintf("%d", navigationScriptLibraryObj.GetGeneration())

			var scriptLibrary kdexv1alpha1.KDexScriptLibrarySpec

			switch v := navigationScriptLibraryObj.(type) {
			case *kdexv1alpha1.KDexScriptLibrary:
				scriptLibrary = v.Spec
			case *kdexv1alpha1.KDexClusterScriptLibrary:
				scriptLibrary = v.Spec
			}

			scriptLibraries = append(scriptLibraries, scriptLibrary)
		}
	}

	parentPageObj, shouldReturn, r1, err := ResolvePageBinding(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageBinding.Spec.ParentPageRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if parentPageObj != nil {
		pageBinding.Status.Attributes["parent.pageBinding.generation"] = fmt.Sprintf("%d", parentPageObj.GetGeneration())
	}

	headerRef := pageBinding.Spec.OverrideHeaderRef
	if headerRef == nil {
		headerRef = pageArchetypeSpec.DefaultHeaderRef
	}
	headerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, headerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	var header *kdexv1alpha1.KDexPageHeaderSpec

	if headerObj != nil {
		pageBinding.Status.Attributes["header.generation"] = fmt.Sprintf("%d", headerObj.GetGeneration())

		switch v := headerObj.(type) {
		case *kdexv1alpha1.KDexPageHeader:
			header = &v.Spec
		case *kdexv1alpha1.KDexClusterPageHeader:
			header = &v.Spec
		}

		headerScriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, header.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if headerScriptLibraryObj != nil {
			pageBinding.Status.Attributes["header.scriptLibrary.generation"] = fmt.Sprintf("%d", headerScriptLibraryObj.GetGeneration())

			var scriptLibrary kdexv1alpha1.KDexScriptLibrarySpec

			switch v := headerScriptLibraryObj.(type) {
			case *kdexv1alpha1.KDexScriptLibrary:
				scriptLibrary = v.Spec
			case *kdexv1alpha1.KDexClusterScriptLibrary:
				scriptLibrary = v.Spec
			}

			scriptLibraries = append(scriptLibraries, scriptLibrary)
		}
	}

	footerRef := pageBinding.Spec.OverrideFooterRef
	if footerRef == nil {
		footerRef = pageArchetypeSpec.DefaultFooterRef
	}
	footerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, footerRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	var footer *kdexv1alpha1.KDexPageFooterSpec

	if footerObj != nil {
		pageBinding.Status.Attributes["footer.generation"] = fmt.Sprintf("%d", footerObj.GetGeneration())

		switch v := footerObj.(type) {
		case *kdexv1alpha1.KDexPageFooter:
			footer = &v.Spec
		case *kdexv1alpha1.KDexClusterPageFooter:
			footer = &v.Spec
		}

		footerScriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, footer.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if footerScriptLibraryObj != nil {
			pageBinding.Status.Attributes["footer.scriptLibrary.generation"] = fmt.Sprintf("%d", footerScriptLibraryObj.GetGeneration())

			var scriptLibrary kdexv1alpha1.KDexScriptLibrarySpec

			switch v := footerScriptLibraryObj.(type) {
			case *kdexv1alpha1.KDexScriptLibrary:
				scriptLibrary = v.Spec
			case *kdexv1alpha1.KDexClusterScriptLibrary:
				scriptLibrary = v.Spec
			}

			scriptLibraries = append(scriptLibraries, scriptLibrary)
		}
	}

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, pageBinding, &pageBinding.Status.Conditions, pageBinding.Spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		pageBinding.Status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())

		var scriptLibrary kdexv1alpha1.KDexScriptLibrarySpec

		switch v := scriptLibraryObj.(type) {
		case *kdexv1alpha1.KDexScriptLibrary:
			scriptLibrary = v.Spec
		case *kdexv1alpha1.KDexClusterScriptLibrary:
			scriptLibrary = v.Spec
		}

		scriptLibraries = append(scriptLibraries, scriptLibrary)
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

		return ctrl.Result{}, err
	}

	packageReferences := []kdexv1alpha1.PackageReference{}
	for _, content := range contents {
		if content.App != nil {
			packageReferences = append(packageReferences, content.App.PackageReference)
		}
	}
	for _, scriptLibrary := range scriptLibraries {
		if scriptLibrary.PackageReference != nil {
			packageReferences = append(packageReferences, *scriptLibrary.PackageReference)
		}
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

		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	hostHandler.Pages.Set(page.PageHandler{
		Archetype:         &pageArchetypeSpec,
		Content:           contents,
		Footer:            footer,
		Header:            header,
		Navigations:       navigations,
		PackageReferences: packageReferences,
		Page:              pageBinding,
		ScriptLibraries:   scriptLibraries,
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

	log.Info("reconciled KDexPageBinding")

	return ctrl.Result{}, nil
}
