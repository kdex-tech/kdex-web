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
	"k8s.io/apimachinery/pkg/util/intstr"
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
	kdexWeb                     = "kdex-web"
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
	log := logf.FromContext(ctx)

	var hostController kdexv1alpha1.KDexHostController
	if err := r.Get(ctx, req.NamespacedName, &hostController); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Defer status update
	defer func() {
		hostController.Status.ObservedGeneration = hostController.Generation
		if updateErr := r.Status().Update(ctx, &hostController); updateErr != nil {
			if err == nil {
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

	r.HostStore.GetOrUpdate(&hostController, scriptLibraries, theme, log)

	// hasRootPage := hasRootPage(hostHandler.Pages)

	// if hostHandler.Pages.Count() == 0 || !hasRootPage {
	// 	err := fmt.Errorf("no pages to render for host %s", host.Name)

	// 	if !hasRootPage {
	// 		err = fmt.Errorf("no root page for host %s", host.Name)
	// 	}

	// 	kdexv1alpha1.SetConditions(
	// 		&hostController.Status.Conditions,
	// 		kdexv1alpha1.ConditionStatuses{
	// 			Degraded:    metav1.ConditionTrue,
	// 			Progressing: metav1.ConditionFalse,
	// 			Ready:       metav1.ConditionFalse,
	// 		},
	// 		kdexv1alpha1.ConditionReasonReconcileError,
	// 		err.Error(),
	// 	)
	// 	return ctrl.Result{}, err
	// }

	return ctrl.Result{}, r.innerReconcile(ctx, &hostController, theme)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexHostControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasFocalHost := func(o client.Object) bool {
		switch t := o.(type) {
		case *kdexv1alpha1.KDexHostController:
			return t.Name == r.FocalHost
		case *kdexv1alpha1.KDexPageBinding:
			return t.Spec.HostRef.Name == r.FocalHost
		case *kdexv1alpha1.KDexScriptLibrary:
			return true // we can't know so we need to accept it
		case *kdexv1alpha1.KDexTheme:
			return true // we can't know so we need to accept it
		default:
			return false
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
		Owns(&networkingv1.Ingress{}).
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

	r.memoizedDeployment = r.Configuration.ThemeServer.Deployment.DeepCopy()

	return r.memoizedDeployment
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

	r.memoizedService = r.Configuration.ThemeServer.Service.DeepCopy()

	return r.memoizedService
}

func (r *KDexHostControllerReconciler) innerReconcile(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	theme *kdexv1alpha1.KDexTheme,
) error {
	log := logf.FromContext(ctx)

	if err := r.createOrUpdatePackageReferences(ctx, hostController); err != nil {
		return err
	}

	if err := r.createOrUpdateThemeDeployment(ctx, hostController, theme); err != nil {
		return err
	}

	if err := r.createOrUpdateThemeService(ctx, hostController, theme); err != nil {
		return err
	}

	if hostController.Spec.Host.Routing.Strategy == kdexv1alpha1.HTTPRouteRoutingStrategy {
		if err := r.createOrUpdateHTTPRoute(ctx, hostController, theme); err != nil {
			return err
		}
	} else {
		if err := r.createOrUpdateIngress(ctx, hostController, theme); err != nil {
			return err
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

	return nil
}

func (r *KDexHostControllerReconciler) createOrUpdatePackageReferences(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
) error {
	hostHandler, ok := r.HostStore.Get(hostController.Name)
	if !ok {
		return nil
	}

	allPackageReferences := []kdexv1alpha1.PackageReference{}
	for _, scriptLibrary := range hostHandler.ScriptLibraries {
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

	hostPackageReferences := &kdexv1alpha1.KDexHostPackageReferences{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostController.Name,
			Namespace: hostController.Namespace,
		},
	}

	if len(finalPackageReferences) == 0 {
		if err := r.Delete(ctx, hostPackageReferences); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return err
			}
		}

		return nil
	}

	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, hostPackageReferences, func() error {
		hostPackageReferences.Spec.PackageReferences = finalPackageReferences
		return ctrl.SetControllerReference(hostController, hostPackageReferences, r.Scheme)
	}); err != nil {
		return err
	}

	return nil
}

func (r *KDexHostControllerReconciler) createOrUpdateIngress(
	ctx context.Context,
	hostController *kdexv1alpha1.KDexHostController,
	theme *kdexv1alpha1.KDexTheme,
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
												Name: hostController.Name,
											},
										},
									},
								},
							},
						},
					},
				})
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
										Name: theme.Name,
									},
								},
							},
						},
					)
				}
			}

			ingress.Annotations = hostController.Annotations
			ingress.Labels = hostController.Labels
			if ingress.Labels == nil {
				ingress.Labels = make(map[string]string)
			}
			ingress.Labels["app.kubernetes.io/name"] = kdexWeb
			ingress.Labels["kdex.dev/focus-host"] = hostController.Name
			ingress.Spec = r.Configuration.ThemeServer.Ingress

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
) error {
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
			if deployment.Labels == nil {
				deployment.Labels = make(map[string]string)
			}
			for key, value := range theme.Labels {
				deployment.Labels[key] = value
			}
			deployment.Labels["app.kubernetes.io/name"] = kdexWeb
			deployment.Labels["kdex.dev/theme"] = theme.Name
			deployment.Spec = *r.getMemoizedDeployment()
			if deployment.Spec.Selector == nil {
				deployment.Spec.Selector = &metav1.LabelSelector{
					MatchLabels: map[string]string{},
				}
			}
			deployment.Spec.Selector.MatchLabels["app.kubernetes.io/name"] = kdexWeb
			deployment.Spec.Selector.MatchLabels["kdex.dev/theme"] = theme.Name

			if deployment.Spec.Template.Labels == nil {
				deployment.Spec.Template.Labels = make(map[string]string)
			}
			deployment.Spec.Template.Labels["app.kubernetes.io/name"] = kdexWeb
			deployment.Spec.Template.Labels["kdex.dev/theme"] = theme.Name

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
			deployment.Spec.Template.Spec.Containers[0].Ports[0].Name = theme.Name

			for idx, value := range deployment.Spec.Template.Spec.Volumes {
				if value.Name == "theme-oci-image" {
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
			if service.Labels == nil {
				service.Labels = make(map[string]string)
			}
			for key, value := range theme.Labels {
				service.Labels[key] = value
			}
			service.Labels["app.kubernetes.io/name"] = kdexWeb
			service.Labels["kdex.dev/theme"] = theme.Name

			service.Spec = *r.getMemoizedService()

			if service.Spec.Selector == nil {
				service.Spec.Selector = make(map[string]string)
			}

			service.Spec.Selector["app.kubernetes.io/name"] = kdexWeb
			service.Spec.Selector["kdex.dev/theme"] = theme.Name

			portFound := false
			for idx, value := range service.Spec.Ports {
				if value.Name == "webserver" || value.Name == theme.Name {
					service.Spec.Ports[idx].Name = theme.Name
					service.Spec.Ports[idx].Port = 80
					service.Spec.Ports[idx].Protocol = corev1.ProtocolTCP
					service.Spec.Ports[idx].TargetPort.StrVal = theme.Name
					portFound = true
				}
			}

			if !portFound {
				service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
					Name:       theme.Name,
					Port:       80,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(theme.Name),
				})
			}

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

// func hasRootPage(pageStore *store.PageStore) bool {
// 	for _, handler := range pageStore.List() {
// 		if handler.Page.Spec.BasePath == "/" {
// 			return true
// 		}
// 	}

// 	return false
// }
