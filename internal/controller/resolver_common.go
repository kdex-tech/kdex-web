package controller

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func resolveHost(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	hostRef *v1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexHost, bool, ctrl.Result, error) {
	var host kdexv1alpha1.KDexHost
	hostName := types.NamespacedName{
		Name:      hostRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, hostName, &host); err != nil {
		if errors.IsNotFound(err) {
			apimeta.SetStatusCondition(
				objectConditions,
				*kdexv1alpha1.NewCondition(
					kdexv1alpha1.ConditionTypeReady,
					metav1.ConditionFalse,
					kdexv1alpha1.ConditionReasonReconcileError,
					fmt.Sprintf("referenced KDexHost %s not found", hostName.Name),
				),
			)
			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}

		return nil, true, ctrl.Result{}, err
	}

	if isReady, r1, err := isReady(ctx, c, object, &host, &host.Status.Conditions, requeueDelay); !isReady {
		return nil, true, r1, err
	}

	return &host, false, ctrl.Result{}, nil
}

func resolveStylesheet(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	stylesheetRef *v1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexStylesheet, bool, ctrl.Result, error) {
	var stylesheet kdexv1alpha1.KDexStylesheet
	if stylesheetRef != nil {
		stylesheetName := types.NamespacedName{
			Name:      stylesheetRef.Name,
			Namespace: object.GetNamespace(),
		}
		if err := c.Get(ctx, stylesheetName, &stylesheet); err != nil {
			if errors.IsNotFound(err) {
				apimeta.SetStatusCondition(
					objectConditions,
					*kdexv1alpha1.NewCondition(
						kdexv1alpha1.ConditionTypeReady,
						metav1.ConditionFalse,
						kdexv1alpha1.ConditionReasonReconcileError,
						fmt.Sprintf("referenced KDexStylesheet %s not found", stylesheetName.Name),
					),
				)
				if err := c.Status().Update(ctx, object); err != nil {
					return nil, true, ctrl.Result{}, err
				}

				return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
			}

			return nil, true, ctrl.Result{}, err
		}

		if isReady, r1, err := isReady(ctx, c, object, &stylesheet, &stylesheet.Status.Conditions, requeueDelay); !isReady {
			return nil, true, r1, err
		}
	}

	return &stylesheet, false, ctrl.Result{}, nil
}
