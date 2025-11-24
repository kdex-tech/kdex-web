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
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	"kdex.dev/web/internal/host"
	"kdex.dev/web/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	cr_handler "sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	hostControllerFinalizerName = "kdex.dev/kdex-web-host-controller-finalizer"
)

// KDexHostControllerReconciler reconciles a KDexHostController object
type KDexHostControllerReconciler struct {
	client.Client
	Configuration       configuration.NexusConfiguration
	ControllerNamespace string
	FocalHost           string
	HostStore           *host.HostStore
	Port                int32
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
	ServiceName         string

	mu                 sync.RWMutex
	memoizedDeployment *appsv1.DeploymentSpec
	memoizedIngress    *networkingv1.IngressSpec
	memoizedService    *corev1.ServiceSpec
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhostcontrollers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhostcontrollers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhostcontrollers/finalizers,verbs=update
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhostpackagereferences,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexpagebindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexscriptlibraries,verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexthemes,verbs=get;list;watch

func (r *KDexHostControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var hostController kdexv1alpha1.KDexHostController
	if err := r.Get(ctx, req.NamespacedName, &hostController); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Defer status update
	defer func() {
		hostController.Status.ObservedGeneration = hostController.Generation
		if updateErr := r.Status().Update(ctx, &hostController); updateErr != nil {
			if res == (ctrl.Result{}) {
				err = updateErr
			}
		}
	}()

	if hostController.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&hostController, hostControllerFinalizerName) {
			controllerutil.AddFinalizer(&hostController, hostControllerFinalizerName)
			if err := r.Update(ctx, &hostController); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&hostController, hostControllerFinalizerName) {
			r.HostStore.Delete(hostController.Name)

			controllerutil.RemoveFinalizer(&hostController, hostControllerFinalizerName)
			if err := r.Update(ctx, &hostController); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	kdexv1alpha1.SetConditions(
		&hostController.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		"Reconciling",
	)

	theme, shouldReturn, r1, err := resolveTheme(ctx, r.Client, &hostController, &hostController.Status.Conditions, hostController.Spec.Host.DefaultThemeRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	var scriptLibraries []kdexv1alpha1.KDexScriptLibrary

	scriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, &hostController, &hostController.Status.Conditions, hostController.Spec.Host.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibrary != nil {
		scriptLibraries = append(scriptLibraries, *scriptLibrary)
	}

	if theme != nil {
		themeScriptLibrary, shouldReturn, r1, err := resolveScriptLibrary(ctx, r.Client, &hostController, &hostController.Status.Conditions, theme.Spec.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if themeScriptLibrary != nil {
			scriptLibraries = append(scriptLibraries, *themeScriptLibrary)
		}
	}

	hostHandler := r.HostStore.GetOrUpdate(hostController.Name)

	allPackageReferences := []kdexv1alpha1.PackageReference{}
	for _, scriptLibrary := range scriptLibraries {
		if scriptLibrary.Spec.PackageReference != nil {
			allPackageReferences = append(allPackageReferences, *scriptLibrary.Spec.PackageReference)
		}
	}

	for _, p := range hostHandler.Pages.List() {
		allPackageReferences = append(allPackageReferences, p.PackageReferences...)
	}

	uniquePackageReferences := make(map[string]kdexv1alpha1.PackageReference)
	for _, pkgRef := range allPackageReferences {
		uniquePackageReferences[pkgRef.Name+"@"+pkgRef.Version] = pkgRef
	}

	finalPackageReferences := []kdexv1alpha1.PackageReference{}
	for _, pkgRef := range uniquePackageReferences {
		finalPackageReferences = append(finalPackageReferences, pkgRef)
	}

	hostPackageReferences, shouldReturn, r1, err := r.createOrUpdatePackageReferences(ctx, &hostController, finalPackageReferences)
	if shouldReturn {
		return r1, err
	}

	if len(finalPackageReferences) == 0 && hostPackageReferences != nil {
		if err := r.Delete(ctx, hostPackageReferences); err != nil {
			if client.IgnoreNotFound(err) != nil {
				kdexv1alpha1.SetConditions(
					&hostController.Status.Conditions,
					kdexv1alpha1.ConditionStatuses{
						Degraded:    metav1.ConditionTrue,
						Progressing: metav1.ConditionFalse,
						Ready:       metav1.ConditionFalse,
					},
					kdexv1alpha1.ConditionReasonReconcileSuccess,
					err.Error(),
				)

				return ctrl.Result{}, err
			}
		}

		hostPackageReferences = nil
	}

	hostHandler.SetHost(
		&hostController.Spec.Host, theme.Spec.Assets, scriptLibraries,
		hostPackageReferences.Status.Attributes["importmap"],
	)

	return r.innerReconcile(ctx, &hostController, theme, hostPackageReferences)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexHostControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasFocalHost := func(o client.Object) bool {
		switch t := o.(type) {
		case *kdexv1alpha1.KDexHostController:
			return t.Name == r.FocalHost
		case *kdexv1alpha1.KDexHostPackageReferences:
			return t.Name == r.FocalHost
		case *kdexv1alpha1.KDexPageBinding:
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
		For(&kdexv1alpha1.KDexHostController{}).
		WithEventFilter(enabledFilter).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Owns(&kdexv1alpha1.KDexHostPackageReferences{}).
		Owns(&networkingv1.Ingress{}).
		Watches(
			&kdexv1alpha1.KDexHostPackageReferences{},
			cr_handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &kdexv1alpha1.KDexHostController{}, cr_handler.OnlyControllerOwner()),
		).
		Watches(
			&kdexv1alpha1.KDexPageBinding{},
			cr_handler.EnqueueRequestsFromMapFunc(r.findHostControllersForPageBinding),
		).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			cr_handler.EnqueueRequestsFromMapFunc(r.findHostControllersForScriptLibrary),
		).
		Watches(
			&kdexv1alpha1.KDexTheme{},
			cr_handler.EnqueueRequestsFromMapFunc(r.findHostControllersForTheme)).
		Named("kdexhostcontroller").
		Complete(r)
}

func (r *KDexHostControllerReconciler) getMemoizedDeployment() *appsv1.DeploymentSpec {
	r.mu.RLock()

	if r.memoizedDeployment != nil {
		r.mu.RUnlock()
		return r.memoizedDeployment
	}

	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.memoizedDeployment = r.Configuration.StaticServing.Deployment.DeepCopy()

	return r.memoizedDeployment
}

func (r *KDexHostControllerReconciler) getMemoizedIngress() *networkingv1.IngressSpec {
	r.mu.RLock()

	if r.memoizedIngress != nil {
		r.mu.RUnlock()
		return r.memoizedIngress
	}

	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.memoizedIngress = r.Configuration.StaticServing.Ingress.DeepCopy()

	return r.memoizedIngress
}

func (r *KDexHostControllerReconciler) getMemoizedService() *corev1.ServiceSpec {
	r.mu.RLock()

	if r.memoizedService != nil {
		r.mu.RUnlock()
		return r.memoizedService
	}

	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.memoizedService = r.Configuration.StaticServing.Service.DeepCopy()

	return r.memoizedService
}

func (r *KDexHostControllerReconciler) innerReconcile(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	theme *kdexv1alpha1.KDexTheme,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if err := r.createOrUpdatePackagesDeployment(ctx, hostController, hostPackageReferences); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.createOrUpdatePackagesService(ctx, hostController, hostPackageReferences); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.createOrUpdateThemeDeployment(ctx, hostController, theme); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.createOrUpdateThemeService(ctx, hostController, theme); err != nil {
		return ctrl.Result{}, err
	}

	if hostController.Spec.Host.Routing.Strategy == kdexv1alpha1.HTTPRouteRoutingStrategy {
		if err := r.createOrUpdateHTTPRoute(ctx, hostController, theme, hostPackageReferences); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.createOrUpdateIngress(ctx, hostController, theme, hostPackageReferences); err != nil {
			return ctrl.Result{}, err
		}
	}

	kdexv1alpha1.SetConditions(
		&hostController.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	log.Info("reconciled KDexHostController")

	return ctrl.Result{}, nil
}

func (r *KDexHostControllerReconciler) createOrUpdatePackageReferences(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	packageReferences []kdexv1alpha1.PackageReference,
) (*kdexv1alpha1.KDexHostPackageReferences, bool, ctrl.Result, error) {
	hostPackageReferences := &kdexv1alpha1.KDexHostPackageReferences{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostController.Name + "-packages",
			Namespace: hostController.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		hostPackageReferences,
		func() error {
			if hostPackageReferences.Annotations == nil {
				hostPackageReferences.Annotations = make(map[string]string)
			}
			for key, value := range hostController.Annotations {
				hostPackageReferences.Annotations[key] = value
			}
			if hostPackageReferences.Labels == nil {
				hostPackageReferences.Labels = make(map[string]string)
			}
			for key, value := range hostController.Labels {
				hostPackageReferences.Labels[key] = value
			}

			hostPackageReferences.Labels["kdex.dev/packages"] = hostPackageReferences.Name

			hostPackageReferences.Spec.PackageReferences = packageReferences

			return ctrl.SetControllerReference(hostController, hostPackageReferences, r.Scheme)
		},
	); err != nil {
		kdexv1alpha1.SetConditions(
			&hostController.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			err.Error(),
		)

		return nil, true, ctrl.Result{}, err
	}

	if hostPackageReferences.Status.Attributes["image"] == "" ||
		hostPackageReferences.Status.Attributes["importmap"] == "" {

		kdexv1alpha1.SetConditions(
			&hostController.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionFalse,
				Progressing: metav1.ConditionTrue,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			"image not available yet, requeueing",
		)

		return nil, true, ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	return hostPackageReferences, false, ctrl.Result{}, nil
}

func (r *KDexHostControllerReconciler) createOrUpdateIngress(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	theme *kdexv1alpha1.KDexTheme,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) error {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostController.Name,
			Namespace: hostController.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		ingress,
		func() error {
			pathType := networkingv1.PathTypePrefix
			rules := make([]networkingv1.IngressRule, 0, len(hostController.Spec.Host.Routing.Domains))

			for _, domain := range hostController.Spec.Host.Routing.Domains {
				rules = append(rules, networkingv1.IngressRule{
					Host: domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: r.ServiceName,
											Port: networkingv1.ServiceBackendPort{
												Name: "server",
											},
										},
									},
								},
							},
						},
					},
				})
			}

			if hostPackageReferences != nil {
				for _, rule := range rules {
					rule.HTTP.Paths = append(rule.HTTP.Paths,
						networkingv1.HTTPIngressPath{
							Path:     "/modules",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: hostPackageReferences.Name,
									Port: networkingv1.ServiceBackendPort{
										Name: "server",
									},
								},
							},
						},
					)
				}
			}

			if theme != nil {
				for _, rule := range rules {
					rule.HTTP.Paths = append(rule.HTTP.Paths,
						networkingv1.HTTPIngressPath{
							Path:     theme.Spec.RoutePath,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: theme.Name,
									Port: networkingv1.ServiceBackendPort{
										Name: "server",
									},
								},
							},
						},
					)
				}
			}

			if ingress.Annotations == nil {
				ingress.Annotations = make(map[string]string)
			}
			for key, value := range hostController.Annotations {
				ingress.Annotations[key] = value
			}
			if ingress.Labels == nil {
				ingress.Labels = make(map[string]string)
			}
			for key, value := range hostController.Labels {
				ingress.Labels[key] = value
			}

			ingress.Spec = *r.getMemoizedIngress().DeepCopy()

			if ingress.Spec.DefaultBackend == nil {
				ingress.Spec.DefaultBackend = &networkingv1.IngressBackend{}
			}

			if ingress.Spec.DefaultBackend.Service == nil {
				ingress.Spec.DefaultBackend.Service = &networkingv1.IngressServiceBackend{}
			}

			ingress.Spec.DefaultBackend.Service.Name = r.ServiceName

			ingress.Spec.DefaultBackend.Service.Port.Name = hostController.Name
			ingress.Spec.IngressClassName = hostController.Spec.Host.Routing.IngressClassName

			ingress.Spec.Rules = append(ingress.Spec.Rules, rules...)

			if hostController.Spec.Host.Routing.TLS != nil {
				ingress.Spec.TLS = append(ingress.Spec.TLS, networkingv1.IngressTLS{
					Hosts:      hostController.Spec.Host.Routing.Domains,
					SecretName: hostController.Spec.Host.Routing.TLS.SecretName,
				})
			}

			return ctrl.SetControllerReference(hostController, ingress, r.Scheme)
		},
	); err != nil {
		kdexv1alpha1.SetConditions(
			&hostController.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return err
	}

	return nil
}

func (r *KDexHostControllerReconciler) createOrUpdateHTTPRoute(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	theme *kdexv1alpha1.KDexTheme,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) error {
	return nil
}

func (r *KDexHostControllerReconciler) createOrUpdatePackagesDeployment(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) error {
	if hostPackageReferences == nil {
		return nil
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostPackageReferences.Name,
			Namespace: hostController.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		deployment,
		func() error {
			if deployment.Annotations == nil {
				deployment.Annotations = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Annotations {
				deployment.Annotations[key] = value
			}
			if deployment.Labels == nil {
				deployment.Labels = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Labels {
				deployment.Labels[key] = value
			}

			deployment.Spec = *r.getMemoizedDeployment().DeepCopy()

			if len(deployment.Spec.Selector.MatchLabels) == 0 {
				deployment.Spec.Selector.MatchLabels["kdex.dev/packages"] = hostPackageReferences.Name
			}

			if len(deployment.Spec.Template.Labels) == 0 {
				deployment.Spec.Template.Labels["kdex.dev/packages"] = hostPackageReferences.Name
			}

			foundCorsDomainsEnv := false
			for idx, value := range deployment.Spec.Template.Spec.Containers[0].Env {
				if value.Name == "CORS_DOMAINS" {
					deployment.Spec.Template.Spec.Containers[0].Env[idx].Value = utils.DomainsToMatcher(hostController.Spec.Host.Routing.Domains)
					foundCorsDomainsEnv = true
				}
			}

			if !foundCorsDomainsEnv {
				deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
					Name:  "CORS_DOMAINS",
					Value: utils.DomainsToMatcher(hostController.Spec.Host.Routing.Domains),
				})
			}

			deployment.Spec.Template.Spec.Containers[0].Name = hostPackageReferences.Name

			for idx, value := range deployment.Spec.Template.Spec.Volumes {
				if value.Name == "oci-image" {
					deployment.Spec.Template.Spec.Volumes[idx].Image.Reference = hostPackageReferences.Status.Attributes["image"]
					deployment.Spec.Template.Spec.Volumes[idx].Image.PullPolicy = corev1.PullIfNotPresent
				}
			}

			return ctrl.SetControllerReference(hostController, deployment, r.Scheme)
		},
	); err != nil {
		kdexv1alpha1.SetConditions(
			&hostController.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return err
	}

	return nil
}

func (r *KDexHostControllerReconciler) createOrUpdatePackagesService(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) error {
	if hostPackageReferences == nil {
		return nil
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostPackageReferences.Name,
			Namespace: hostController.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		service,
		func() error {
			if service.Annotations == nil {
				service.Annotations = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Annotations {
				service.Annotations[key] = value
			}
			if service.Labels == nil {
				service.Labels = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Labels {
				service.Labels[key] = value
			}

			service.Spec = *r.getMemoizedService().DeepCopy()

			if service.Spec.Selector == nil {
				service.Spec.Selector = make(map[string]string)
			}

			service.Spec.Selector["kdex.dev/packages"] = hostPackageReferences.Name

			return ctrl.SetControllerReference(hostController, service, r.Scheme)
		},
	); err != nil {
		kdexv1alpha1.SetConditions(
			&hostController.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return err
	}

	return nil
}

func (r *KDexHostControllerReconciler) createOrUpdateThemeDeployment(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	theme *kdexv1alpha1.KDexTheme,
) error {
	if theme == nil {
		return nil
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      theme.Name,
			Namespace: hostController.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		deployment,
		func() error {
			if deployment.Annotations == nil {
				deployment.Annotations = make(map[string]string)
			}
			for key, value := range theme.Annotations {
				deployment.Annotations[key] = value
			}
			for key, value := range hostController.Annotations {
				deployment.Annotations[key] = value
			}
			if deployment.Labels == nil {
				deployment.Labels = make(map[string]string)
			}
			for key, value := range theme.Labels {
				deployment.Labels[key] = value
			}
			for key, value := range hostController.Labels {
				deployment.Labels[key] = value
			}

			deployment.Labels["kdex.dev/theme"] = theme.Name

			deployment.Spec = *r.getMemoizedDeployment().DeepCopy()

			if len(deployment.Spec.Selector.MatchLabels) == 0 {
				deployment.Spec.Selector.MatchLabels["kdex.dev/theme"] = theme.Name
			}

			if len(deployment.Spec.Template.Labels) == 0 {
				deployment.Spec.Template.Labels["kdex.dev/theme"] = theme.Name
			}

			foundCorsDomainsEnv := false
			for idx, value := range deployment.Spec.Template.Spec.Containers[0].Env {
				if value.Name == "CORS_DOMAINS" {
					deployment.Spec.Template.Spec.Containers[0].Env[idx].Value = utils.DomainsToMatcher(hostController.Spec.Host.Routing.Domains)
					foundCorsDomainsEnv = true
				}
			}

			if !foundCorsDomainsEnv {
				deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
					Name:  "CORS_DOMAINS",
					Value: utils.DomainsToMatcher(hostController.Spec.Host.Routing.Domains),
				})
			}

			deployment.Spec.Template.Spec.Containers[0].Name = theme.Name

			for idx, value := range deployment.Spec.Template.Spec.Volumes {
				if value.Name == "oci-image" {
					deployment.Spec.Template.Spec.Volumes[idx].Image.Reference = theme.Spec.Image

					if theme.Spec.PullPolicy != "" {
						deployment.Spec.Template.Spec.Volumes[idx].Image.PullPolicy = theme.Spec.PullPolicy
					}
				}
			}

			return ctrl.SetControllerReference(hostController, deployment, r.Scheme)
		},
	); err != nil {
		kdexv1alpha1.SetConditions(
			&hostController.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return err
	}

	return nil
}

func (r *KDexHostControllerReconciler) createOrUpdateThemeService(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	theme *kdexv1alpha1.KDexTheme,
) error {
	if theme == nil {
		return nil
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      theme.Name,
			Namespace: hostController.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		service,
		func() error {
			if service.Annotations == nil {
				service.Annotations = make(map[string]string)
			}
			for key, value := range theme.Annotations {
				service.Annotations[key] = value
			}
			for key, value := range hostController.Annotations {
				service.Annotations[key] = value
			}
			if service.Labels == nil {
				service.Labels = make(map[string]string)
			}
			for key, value := range theme.Labels {
				service.Labels[key] = value
			}
			for key, value := range hostController.Labels {
				service.Labels[key] = value
			}

			service.Labels["kdex.dev/theme"] = theme.Name

			service.Spec = *r.getMemoizedService().DeepCopy()

			if service.Spec.Selector == nil {
				service.Spec.Selector = make(map[string]string)
			}

			service.Spec.Selector["kdex.dev/theme"] = theme.Name

			return ctrl.SetControllerReference(hostController, service, r.Scheme)
		},
	); err != nil {
		kdexv1alpha1.SetConditions(
			&hostController.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return err
	}

	return nil
}
