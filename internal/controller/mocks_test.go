package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MockHostReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MockHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var host kdexv1alpha1.KDexHost
	if err := r.Get(ctx, req.NamespacedName, &host); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	kdexv1alpha1.SetConditions(
		&host.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)
	host.Status.ObservedGeneration = host.Generation
	if err := r.Status().Update(ctx, &host); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MockHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexHost{}).
		Named("mockhostreconciler").
		Complete(r)
}

type MockPageArchetypeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MockPageArchetypeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pageArchetype kdexv1alpha1.KDexPageArchetype
	if err := r.Get(ctx, req.NamespacedName, &pageArchetype); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	kdexv1alpha1.SetConditions(
		&pageArchetype.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)
	pageArchetype.Status.ObservedGeneration = pageArchetype.Generation
	if err := r.Status().Update(ctx, &pageArchetype); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MockPageArchetypeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageArchetype{}).
		Named("mockpagearchetypereconciler").
		Complete(r)
}

type MockPageFooterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MockPageFooterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pageFooter kdexv1alpha1.KDexPageFooter
	if err := r.Get(ctx, req.NamespacedName, &pageFooter); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	kdexv1alpha1.SetConditions(
		&pageFooter.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)
	pageFooter.Status.ObservedGeneration = pageFooter.Generation
	if err := r.Status().Update(ctx, &pageFooter); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MockPageFooterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageFooter{}).
		Named("mockpagefooterreconciler").
		Complete(r)
}

type MockPageHeaderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MockPageHeaderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pageHeader kdexv1alpha1.KDexPageHeader
	if err := r.Get(ctx, req.NamespacedName, &pageHeader); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	kdexv1alpha1.SetConditions(
		&pageHeader.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)
	pageHeader.Status.ObservedGeneration = pageHeader.Generation
	if err := r.Status().Update(ctx, &pageHeader); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MockPageHeaderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageHeader{}).
		Named("mockpageheaderreconciler").
		Complete(r)
}

type MockPageNavigationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MockPageNavigationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pageNavigation kdexv1alpha1.KDexPageNavigation
	if err := r.Get(ctx, req.NamespacedName, &pageNavigation); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	kdexv1alpha1.SetConditions(
		&pageNavigation.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)
	pageNavigation.Status.ObservedGeneration = pageNavigation.Generation
	if err := r.Status().Update(ctx, &pageNavigation); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MockPageNavigationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexPageNavigation{}).
		Named("mockpagenavigationreconciler").
		Complete(r)
}
