package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResolvedContentEntry struct {
	Content string
	App     *kdexv1alpha1.KDexApp
}

func resolveContents(
	ctx context.Context,
	c client.Client,
	pageBinding *kdexv1alpha1.KDexPageBinding,
	requeueDelay time.Duration,
) (map[string]ResolvedContentEntry, bool, ctrl.Result, error) {
	contents := make(map[string]ResolvedContentEntry)

	for _, contentEntry := range pageBinding.Spec.ContentEntries {
		appRef := contentEntry.AppRef
		if appRef == nil {
			contents[contentEntry.Slot] = ResolvedContentEntry{
				Content: contentEntry.RawHTML,
			}

			continue
		}

		var app kdexv1alpha1.KDexApp
		appName := types.NamespacedName{
			Name:      appRef.Name,
			Namespace: pageBinding.Namespace,
		}
		if err := c.Get(ctx, appName, &app); err != nil {
			if errors.IsNotFound(err) {
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

				if err := c.Status().Update(ctx, pageBinding); err != nil {
					return nil, true, ctrl.Result{}, err
				}

				return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
			}

			return nil, true, ctrl.Result{}, err
		}

		if isReady, r1, err := isReady(ctx, c, pageBinding, &app, &app.Status.Conditions, requeueDelay); !isReady {
			return nil, true, r1, err
		}

		contents[contentEntry.Slot] = ResolvedContentEntry{App: &app}
	}

	return contents, false, ctrl.Result{}, nil
}

func resolveHost(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	hostRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexHost, bool, ctrl.Result, error) {
	if hostRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var host kdexv1alpha1.KDexHost
	hostName := types.NamespacedName{
		Name:      hostRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, hostName, &host); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
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

func resolvePageArchetype(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	pageArchetypeRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexPageArchetype, bool, ctrl.Result, error) {
	if pageArchetypeRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var pageArchetype kdexv1alpha1.KDexPageArchetype
	pageArchetypeName := types.NamespacedName{
		Name:      pageArchetypeRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, pageArchetypeName, &pageArchetype); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)

			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}

		return nil, true, ctrl.Result{}, err
	}

	if isReady, r1, err := isReady(ctx, c, object, &pageArchetype, &pageArchetype.Status.Conditions, requeueDelay); !isReady {
		return nil, true, r1, err
	}

	return &pageArchetype, false, ctrl.Result{}, nil
}

func resolvePageFooter(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	footerRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexPageFooter, bool, ctrl.Result, error) {
	if footerRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var footer kdexv1alpha1.KDexPageFooter
	footerName := types.NamespacedName{
		Name:      footerRef.Name,
		Namespace: object.GetNamespace(),
	}

	if err := c.Get(ctx, footerName, &footer); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)

			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}

		return nil, true, ctrl.Result{}, err
	}

	if isReady, r1, err := isReady(ctx, c, object, &footer, &footer.Status.Conditions, requeueDelay); !isReady {
		return nil, true, r1, err
	}

	return &footer, false, ctrl.Result{}, nil
}

func resolvePageHeader(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	headerRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexPageHeader, bool, ctrl.Result, error) {
	if headerRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var header kdexv1alpha1.KDexPageHeader
	headerName := types.NamespacedName{
		Name:      headerRef.Name,
		Namespace: object.GetNamespace(),
	}

	if err := c.Get(ctx, headerName, &header); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)
			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}

		return nil, true, ctrl.Result{}, err
	}

	if isReady, r1, err := isReady(ctx, c, object, &header, &header.Status.Conditions, requeueDelay); !isReady {
		return nil, true, r1, err
	}

	return &header, false, ctrl.Result{}, nil
}

func resolvePageNavigation(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	navigationRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexPageNavigation, bool, ctrl.Result, error) {
	if navigationRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var navigation kdexv1alpha1.KDexPageNavigation
	navigationName := types.NamespacedName{
		Name:      navigationRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, navigationName, &navigation); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)

			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}

		return nil, true, ctrl.Result{}, err
	}

	if isReady, r1, err := isReady(ctx, c, object, &navigation, &navigation.Status.Conditions, requeueDelay); !isReady {
		return nil, true, r1, err
	}

	return &navigation, false, ctrl.Result{}, nil
}

func resolvePageNavigations(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	navigationRef *corev1.LocalObjectReference,
	extraNavigations map[string]*corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (map[string]*kdexv1alpha1.KDexPageNavigation, bool, ctrl.Result, error) {
	navigations := make(map[string]*kdexv1alpha1.KDexPageNavigation)

	navigation, shouldReturn, response, err := resolvePageNavigation(
		ctx, c, object, objectConditions, navigationRef, requeueDelay)

	if shouldReturn {
		return nil, true, response, err
	}

	if navigation != nil {
		navigations["main"] = navigation
	}

	if extraNavigations == nil {
		extraNavigations = map[string]*corev1.LocalObjectReference{}
	}

	for navigationName, navigationRef := range extraNavigations {
		navigation, shouldReturn, response, err := resolvePageNavigation(
			ctx, c, object, objectConditions, navigationRef, requeueDelay)

		if shouldReturn {
			return nil, true, response, err
		}

		if navigation != nil {
			navigations[navigationName] = navigation
		}
	}

	return navigations, false, ctrl.Result{}, nil
}

func resolvePageBinding(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	pageBindingRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexPageBinding, bool, ctrl.Result, error) {
	if pageBindingRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var pageBinding kdexv1alpha1.KDexPageBinding
	pageBindingName := types.NamespacedName{
		Name:      pageBindingRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, pageBindingName, &pageBinding); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)

			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}

		return nil, true, ctrl.Result{}, err
	}

	if isReady, r1, err := isReady(ctx, c, object, &pageBinding, &pageBinding.Status.Conditions, requeueDelay); !isReady {
		return nil, true, r1, err
	}

	return &pageBinding, false, ctrl.Result{}, nil
}

func resolveScriptLibrary(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	scriptLibraryRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexScriptLibrary, bool, ctrl.Result, error) {
	if scriptLibraryRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var scriptLibrary kdexv1alpha1.KDexScriptLibrary
	scriptLibraryName := types.NamespacedName{
		Name:      scriptLibraryRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, scriptLibraryName, &scriptLibrary); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)

			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}

		return nil, true, ctrl.Result{}, err
	}

	if isReady, r1, err := isReady(ctx, c, object, &scriptLibrary, &scriptLibrary.Status.Conditions, requeueDelay); !isReady {
		return nil, true, r1, err
	}

	return &scriptLibrary, false, ctrl.Result{}, nil
}

func resolveSecret(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	secretRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*corev1.Secret, bool, ctrl.Result, error) {
	if secretRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var secret corev1.Secret
	secretName := types.NamespacedName{
		Name:      secretRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, secretName, &secret); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)

			if err := c.Status().Update(ctx, object); err != nil {
				return nil, true, ctrl.Result{}, err
			}

			return nil, true, ctrl.Result{RequeueAfter: requeueDelay}, nil
		}
	}

	return &secret, false, ctrl.Result{}, nil
}

func resolveTheme(
	ctx context.Context,
	c client.Client,
	object client.Object,
	objectConditions *[]metav1.Condition,
	themeRef *corev1.LocalObjectReference,
	requeueDelay time.Duration,
) (*kdexv1alpha1.KDexTheme, bool, ctrl.Result, error) {
	if themeRef == nil {
		return nil, false, ctrl.Result{}, nil
	}

	var theme kdexv1alpha1.KDexTheme
	themeName := types.NamespacedName{
		Name:      themeRef.Name,
		Namespace: object.GetNamespace(),
	}
	if err := c.Get(ctx, themeName, &theme); err != nil {
		if errors.IsNotFound(err) {
			kdexv1alpha1.SetConditions(
				objectConditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
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

	return &theme, false, ctrl.Result{}, nil
}
