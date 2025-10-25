package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func isReady(
	ctx context.Context,
	c client.Client,
	object client.Object,
	referred client.Object,
	referredConditions *[]metav1.Condition,
	requeueDelay time.Duration,
) (bool, ctrl.Result, error) {
	t := reflect.TypeOf(referred)
	if t == nil {
		return false, ctrl.Result{}, fmt.Errorf("referred is nil")
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if !apimeta.IsStatusConditionTrue(*referredConditions, string(kdexv1alpha1.ConditionTypeReady)) {
		apimeta.SetStatusCondition(
			referredConditions,
			*kdexv1alpha1.NewCondition(
				kdexv1alpha1.ConditionTypeReady,
				metav1.ConditionFalse,
				kdexv1alpha1.ConditionReasonReconcileError,
				fmt.Sprintf("referenced %s %s is not ready", t.Name(), referred.GetName()),
			),
		)
		if err := c.Status().Update(ctx, object); err != nil {
			return false, ctrl.Result{}, err
		}

		return false, ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	return true, ctrl.Result{}, nil
}
