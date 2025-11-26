package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/jsonpath"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var mockHandler = handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: o.GetName()}}}
})

var handlerMaker = func(c client.Client, t client.ObjectList, refPath string) handler.EventHandler {
	jpRef := jsonpath.New("ref-path")
	if err := jpRef.Parse(refPath); err != nil {
		panic(err)
	}
	jpName := jsonpath.New("name-path")
	if err := jpName.Parse("{.Name}"); err != nil {
		panic(err)
	}
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		if err := c.List(ctx, t, &client.ListOptions{
			Namespace: o.GetNamespace(),
		}); err != nil {
			return []reconcile.Request{}
		}

		items, err := meta.ExtractList(t)
		if err != nil {
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0, len(items))
		for _, item := range items {
			i := item.(client.Object)

			refResults, err := jpRef.FindResults(i)
			if err != nil {
				panic(err)
			}

			if len(refResults) == 0 || len(refResults[0]) == 0 {
				continue
			}

			if refResults[0][0].IsNil() {
				continue
			}

			res := refResults[0][0].Interface()

			if res == nil {
				continue
			}

			nameResults, err := jpName.FindResults(res)
			if err != nil {
				panic(err)
			}

			if len(nameResults) == 0 || len(nameResults[0]) == 0 {
				continue
			}

			name := nameResults[0][0].Interface()

			if name == o.GetName() {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      i.GetName(),
						Namespace: i.GetNamespace(),
					},
				})
			}
		}
		return requests
	})
}

type MockPageArchetypeReconciler struct {
	client.Client
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

func (r *MockPageArchetypeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var status *kdexv1alpha1.KDexObjectStatus
	var spec kdexv1alpha1.KDexPageArchetypeSpec
	var om metav1.ObjectMeta
	var o client.Object

	if req.Namespace == "" {
		var clusterPageArchetype kdexv1alpha1.KDexClusterPageArchetype
		if err := r.Get(ctx, req.NamespacedName, &clusterPageArchetype); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &clusterPageArchetype.Status
		spec = clusterPageArchetype.Spec
		om = clusterPageArchetype.ObjectMeta
		o = &clusterPageArchetype
	} else {
		var pageArchetype kdexv1alpha1.KDexPageArchetype
		if err := r.Get(ctx, req.NamespacedName, &pageArchetype); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &pageArchetype.Status
		spec = pageArchetype.Spec
		om = pageArchetype.ObjectMeta
		o = &pageArchetype
	}

	if status.Attributes == nil {
		status.Attributes = make(map[string]string)
	}

	log := logf.Log.WithName("MockPageArchetypeReconciler")

	// Defer status update
	defer func() {
		status.ObservedGeneration = om.Generation
		if updateErr := r.Status().Update(ctx, o); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}
		log.Info("print status", "status", status, "err", err, "res", res)
	}()

	footerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, o, &status.Conditions, spec.DefaultFooterRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if footerObj != nil {
		status.Attributes["footer.generation"] = fmt.Sprintf("%d", footerObj.GetGeneration())
	}

	headerObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, o, &status.Conditions, spec.DefaultHeaderRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if headerObj != nil {
		status.Attributes["header.generation"] = fmt.Sprintf("%d", headerObj.GetGeneration())
	}

	navigations, shouldReturn, response, err := ResolvePageNavigations(ctx, r.Client, o, &status.Conditions, spec.DefaultMainNavigationRef, spec.ExtraNavigations, r.RequeueDelay)
	if shouldReturn {
		return response, err
	}

	for k, navigation := range navigations {
		status.Attributes[k+".navigation.generation"] = fmt.Sprintf("%d", navigation.GetGeneration())
	}

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, o, &status.Conditions, spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())
	}

	kdexv1alpha1.SetConditions(
		&status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	return ctrl.Result{}, nil
}

func (r *MockPageArchetypeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageArchetype{}).
		Watches(
			&kdexv1alpha1.KDexClusterPageArchetype{},
			mockHandler,
		).
		Watches(
			&kdexv1alpha1.KDexPageFooter{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.DefaultFooterRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexClusterPageFooter{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.DefaultFooterRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexPageHeader{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.DefaultHeaderRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexClusterPageHeader{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.DefaultHeaderRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexPageNavigation{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.DefaultMainNavigationRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexClusterPageNavigation{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.DefaultMainNavigationRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageArchetypeList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Named("mockpagearchetypereconciler").
		Complete(r)
}

type MockPageFooterReconciler struct {
	client.Client
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

func (r *MockPageFooterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var status *kdexv1alpha1.KDexObjectStatus
	var spec kdexv1alpha1.KDexPageFooterSpec
	var om metav1.ObjectMeta
	var o client.Object

	if req.Namespace == "" {
		var clusterPageFooter kdexv1alpha1.KDexClusterPageFooter
		if err := r.Get(ctx, req.NamespacedName, &clusterPageFooter); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &clusterPageFooter.Status
		spec = clusterPageFooter.Spec
		om = clusterPageFooter.ObjectMeta
		o = &clusterPageFooter
	} else {
		var pageFooter kdexv1alpha1.KDexPageFooter
		if err := r.Get(ctx, req.NamespacedName, &pageFooter); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &pageFooter.Status
		spec = pageFooter.Spec
		om = pageFooter.ObjectMeta
		o = &pageFooter
	}

	if status.Attributes == nil {
		status.Attributes = make(map[string]string)
	}

	log := logf.Log.WithName("MockPageFooterReconciler")

	// Defer status update
	defer func() {
		status.ObservedGeneration = om.Generation
		if updateErr := r.Status().Update(ctx, o); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}
		log.Info("print status", "status", status, "err", err, "res", res)
	}()

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, o, &status.Conditions, spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())
	}

	kdexv1alpha1.SetConditions(
		&status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	return ctrl.Result{}, nil
}

func (r *MockPageFooterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageFooter{}).
		Watches(
			&kdexv1alpha1.KDexClusterPageFooter{},
			mockHandler,
		).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageFooterList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageFooterList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Named("mockpagefooterreconciler").
		Complete(r)
}

type MockPageHeaderReconciler struct {
	client.Client
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

func (r *MockPageHeaderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var status *kdexv1alpha1.KDexObjectStatus
	var spec kdexv1alpha1.KDexPageHeaderSpec
	var om metav1.ObjectMeta
	var o client.Object

	if req.Namespace == "" {
		var clusterPageHeader kdexv1alpha1.KDexClusterPageHeader
		if err := r.Get(ctx, req.NamespacedName, &clusterPageHeader); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &clusterPageHeader.Status
		spec = clusterPageHeader.Spec
		om = clusterPageHeader.ObjectMeta
		o = &clusterPageHeader
	} else {
		var pageHeader kdexv1alpha1.KDexPageHeader
		if err := r.Get(ctx, req.NamespacedName, &pageHeader); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &pageHeader.Status
		spec = pageHeader.Spec
		om = pageHeader.ObjectMeta
		o = &pageHeader
	}

	if status.Attributes == nil {
		status.Attributes = make(map[string]string)
	}

	log := logf.Log.WithName("MockPageHeaderReconciler")

	// Defer status update
	defer func() {
		status.ObservedGeneration = om.Generation
		if updateErr := r.Status().Update(ctx, o); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}
		log.Info("print status", "status", status, "err", err, "res", res)
	}()

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, o, &status.Conditions, spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())
	}

	kdexv1alpha1.SetConditions(
		&status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	return ctrl.Result{}, nil
}

func (r *MockPageHeaderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageHeader{}).
		Watches(
			&kdexv1alpha1.KDexClusterPageHeader{},
			mockHandler,
		).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageHeaderList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageHeaderList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Named("mockpageheaderreconciler").
		Complete(r)
}

type MockPageNavigationReconciler struct {
	client.Client
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

func (r *MockPageNavigationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var status *kdexv1alpha1.KDexObjectStatus
	var spec kdexv1alpha1.KDexPageNavigationSpec
	var om metav1.ObjectMeta
	var o client.Object

	if req.Namespace == "" {
		var clusterPageNavigation kdexv1alpha1.KDexClusterPageNavigation
		if err := r.Get(ctx, req.NamespacedName, &clusterPageNavigation); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &clusterPageNavigation.Status
		spec = clusterPageNavigation.Spec
		om = clusterPageNavigation.ObjectMeta
		o = &clusterPageNavigation
	} else {
		var pageNavigation kdexv1alpha1.KDexPageNavigation
		if err := r.Get(ctx, req.NamespacedName, &pageNavigation); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &pageNavigation.Status
		spec = pageNavigation.Spec
		om = pageNavigation.ObjectMeta
		o = &pageNavigation
	}

	if status.Attributes == nil {
		status.Attributes = make(map[string]string)
	}

	log := logf.Log.WithName("MockPageNavigationReconciler")

	// Defer status update
	defer func() {
		status.ObservedGeneration = om.Generation
		if updateErr := r.Status().Update(ctx, o); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}
		log.Info("print status", "status", status, "err", err, "res", res)
	}()

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, o, &status.Conditions, spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())
	}

	kdexv1alpha1.SetConditions(
		&status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	return ctrl.Result{}, nil
}

func (r *MockPageNavigationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageNavigation{}).
		Watches(
			&kdexv1alpha1.KDexClusterPageNavigation{},
			mockHandler,
		).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageNavigationList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			handlerMaker(r.Client, &kdexv1alpha1.KDexPageNavigationList{}, "{.Spec.ScriptLibraryRef}"),
		).
		Named("mockpagenavigationreconciler").
		Complete(r)
}

type MockScriptLibraryReconciler struct {
	client.Client
	RequeueDelay time.Duration
	Scheme       *runtime.Scheme
}

func (r *MockScriptLibraryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var status *kdexv1alpha1.KDexObjectStatus
	var spec kdexv1alpha1.KDexScriptLibrarySpec
	var om metav1.ObjectMeta
	var o client.Object

	if req.Namespace == "" {
		var clusterScriptLibrary kdexv1alpha1.KDexClusterScriptLibrary
		if err := r.Get(ctx, req.NamespacedName, &clusterScriptLibrary); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &clusterScriptLibrary.Status
		spec = clusterScriptLibrary.Spec
		om = clusterScriptLibrary.ObjectMeta
		o = &clusterScriptLibrary
	} else {
		var scriptLibrary kdexv1alpha1.KDexScriptLibrary
		if err := r.Get(ctx, req.NamespacedName, &scriptLibrary); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		status = &scriptLibrary.Status
		spec = scriptLibrary.Spec
		om = scriptLibrary.ObjectMeta
		o = &scriptLibrary
	}

	if status.Attributes == nil {
		status.Attributes = make(map[string]string)
	}

	log := logf.Log.WithName("MockScriptLibraryReconciler")

	// Defer status update
	defer func() {
		status.ObservedGeneration = om.Generation
		if updateErr := r.Status().Update(ctx, o); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}
		log.Info("print status", "status", status, "err", err, "res", res)
	}()

	if spec.PackageReference != nil {
		secret, shouldReturn, r1, err := ResolveSecret(ctx, r.Client, o, &status.Conditions, spec.PackageReference.SecretRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if secret != nil {
			status.Attributes["secret.generation"] = fmt.Sprintf("%d", secret.Generation)
		}
	}

	kdexv1alpha1.SetConditions(
		&status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	return ctrl.Result{}, nil
}

func (r *MockScriptLibraryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexScriptLibrary{}).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			mockHandler,
		).
		Watches(
			&corev1.Secret{},
			mockHandler,
		).
		Named("mockscriptlibraryreconciler").
		Complete(r)
}
