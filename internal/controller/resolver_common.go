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

func resolveTheme(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	themeRef *v1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexTheme, bool, ctrl.Result, error) {
	var theme kdexv1alpha1.KDexTheme
	if themeRef != nil {
		themeName := types.NamespacedName{
			Name:      themeRef.Name,
			Namespace: object.GetNamespace(),
		}
		if err := c.Get(ctx, themeName, &theme); err != nil {
			if errors.IsNotFound(err) {
				apimeta.SetStatusCondition(
					objectConditions,
					*kdexv1alpha1.NewCondition(
						kdexv1alpha1.ConditionTypeReady,
						metav1.ConditionFalse,
						kdexv1alpha1.ConditionReasonReconcileError,
						fmt.Sprintf("referenced KDexTheme %s not found", themeName.Name),
					),
				)
				if err := c.Status().Update(ctx, object); err != nil {
					return nil, true, ctrl.Result{}, err
				}

				return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
			}

			return nil, true, ctrl.Result{}, err
		}

		if isReady, r1, err := isReady(ctx, c, object, &theme, &theme.Status.Conditions, requeueDelay); !isReady {
			return nil, true, r1, err
		}
	}

	return &theme, false, ctrl.Result{}, nil
}
