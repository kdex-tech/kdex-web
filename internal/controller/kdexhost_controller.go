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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/store"
	"kdex.dev/web/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const hostFinalizerName = "kdex.dev/kdex-web-host-finalizer"

// KDexHostReconciler reconciles a KDexHost object
type KDexHostReconciler struct {
	client.Client
	ControllerNamespace string
	Defaults            ResourceDefaults
	FocalHost           string
	HostStore           *store.HostStore
	Port                int32
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhosts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhosts/finalizers,verbs=update
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexthemes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KDexHost object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KDexHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var host kdexv1alpha1.KDexHost
	if err := r.Get(ctx, req.NamespacedName, &host); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if host.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&host, hostFinalizerName) {
			controllerutil.AddFinalizer(&host, hostFinalizerName)
			if err := r.Update(ctx, &host); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(&host, hostFinalizerName) {
			r.HostStore.Delete(host.Name)
			controllerutil.RemoveFinalizer(&host, hostFinalizerName)
			if err := r.Update(ctx, &host); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	theme, shouldReturn, r1, err := resolveTheme(ctx, r.Client, &host, &host.Status.Conditions, host.Spec.DefaultThemeRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	hostHandler := r.HostStore.GetOrDefault(
		host.Name, theme, log.WithName("host-handler").WithValues("host", host.Name))
	hostHandler.SetHost(&host, theme)

	// TODO: the host should only become ready and create accompanying resources when the host us fully ready having at
	// least a single root page

	if hostHandler.RenderPages.Count() == 0 {
		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	shouldReturn, r1, err = r.createOrUpdateAccompanyingResources(ctx, &host, theme)

	if shouldReturn {
		return r1, err
	}

	log.Info("reconciled KDexHost")

	apimeta.SetStatusCondition(
		&host.Status.Conditions,
		*kdexv1alpha1.NewCondition(
			kdexv1alpha1.ConditionTypeReady,
			metav1.ConditionTrue,
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			"all references resolved successfully",
		),
	)

	if err := r.Status().Update(ctx, &host); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KDexHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var enabledFilter = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == r.FocalHost
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == r.FocalHost
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == r.FocalHost
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == r.FocalHost
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexHost{}).
		WithEventFilter(enabledFilter).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Watches(
			&kdexv1alpha1.KDexTheme{},
			handler.EnqueueRequestsFromMapFunc(r.findHostsForTheme)).
		Named("kdexhost-" + r.FocalHost).
		Complete(r)
}

func (r *KDexHostReconciler) createOrUpdateAccompanyingResources(
	ctx context.Context,
	host *kdexv1alpha1.KDexHost,
	theme *kdexv1alpha1.KDexTheme,
) (bool, ctrl.Result, error) {
	if theme.Spec.Assets != nil {
		shouldReturn, r1, err := r.createOrUpdateThemeDeployment(ctx, host, theme)

		if shouldReturn {
			return shouldReturn, r1, err
		}

		shouldReturn, r1, err = r.createOrUpdateThemeService(ctx, host, theme)

		if shouldReturn {
			return shouldReturn, r1, err
		}
	}

	if host.Spec.Routing.Strategy == kdexv1alpha1.HTTPRouteRoutingStrategy {
		return r.createOrUpdateHTTPRoute(ctx, host, theme)
	}

	return r.createOrUpdateIngress(ctx, host, theme)
}

func (r *KDexHostReconciler) createOrUpdateIngress(
	ctx context.Context,
	host *kdexv1alpha1.KDexHost,
	theme *kdexv1alpha1.KDexTheme,
) (bool, ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      host.Name,
			Namespace: host.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		ingress,
		func() error {
			serviceName := "kdex-web-controller-manager"
			pathType := networkingv1.PathTypePrefix
			rules := make([]networkingv1.IngressRule, 0, len(host.Spec.Routing.Domains))

			for _, domain := range host.Spec.Routing.Domains {
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
											Name: serviceName,
											Port: networkingv1.ServiceBackendPort{
												Number: r.Port,
											},
										},
									},
								},
							},
						},
					},
				})
			}

			if theme.Spec.Assets != nil {
				for _, rule := range rules {
					rule.IngressRuleValue.HTTP.Paths = append(rule.IngressRuleValue.HTTP.Paths,
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

			ingress.Annotations = host.Annotations
			ingress.Labels = host.Labels
			if ingress.Labels == nil {
				ingress.Labels = make(map[string]string)
			}
			ingress.Labels["kdex.dev/host"] = host.Name
			ingress.Spec = networkingv1.IngressSpec{
				DefaultBackend: &networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: serviceName,
						Port: networkingv1.ServiceBackendPort{
							Number: r.Port,
						},
					},
				},
				IngressClassName: host.Spec.Routing.IngressClassName,
				Rules:            rules,
			}

			if host.Spec.Routing.TLS != nil {
				ingress.Spec.TLS = []networkingv1.IngressTLS{
					{
						Hosts:      host.Spec.Routing.Domains,
						SecretName: host.Name,
					},
				}
			}

			return ctrl.SetControllerReference(host, ingress, r.Scheme)
		},
	); err != nil {
		log.Error(err, "unable to create or update Ingress", "ingress", ingress.Name)
		return true, ctrl.Result{}, err
	}

	return false, ctrl.Result{}, nil
}

func (r *KDexHostReconciler) createOrUpdateHTTPRoute(
	ctx context.Context,
	host *kdexv1alpha1.KDexHost,
	theme *kdexv1alpha1.KDexTheme,
) (bool, ctrl.Result, error) {
	panic("unimplemented")
}

func (r *KDexHostReconciler) createOrUpdateThemeDeployment(
	ctx context.Context,
	host *kdexv1alpha1.KDexHost,
	theme *kdexv1alpha1.KDexTheme,
) (bool, ctrl.Result, error) {
	log := logf.FromContext(ctx)

	defaults := r.Defaults

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      theme.Name,
			Namespace: host.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		deployment,
		func() error {
			replicas := defaults.Deployment.Replicas
			resources := defaults.Container.Resources
			themePullPolicy := defaults.Theme.PullPolicy
			webserverImage := defaults.Deployment.Image
			webserverPullPolicy := defaults.Deployment.PullPolicy

			if theme.Spec.PullPolicy != "" {
				themePullPolicy = theme.Spec.PullPolicy
			}

			if theme.Spec.WebServer != nil {
				if theme.Spec.WebServer.Image != "" {
					webserverImage = theme.Spec.WebServer.Image
				}

				if theme.Spec.WebServer.PullPolicy != "" {
					webserverPullPolicy = theme.Spec.WebServer.PullPolicy
				}

				if theme.Spec.WebServer.PullPolicy != "" {
					webserverPullPolicy = theme.Spec.WebServer.PullPolicy
				}

				if theme.Spec.WebServer.Replicas != nil {
					replicas = theme.Spec.WebServer.Replicas
				}

				if theme.Spec.WebServer.Resources.Limits != nil ||
					theme.Spec.WebServer.Resources.Requests != nil {

					resources = theme.Spec.WebServer.Resources
				}
			}

			scratchSize := resource.MustParse("16Ki")

			deployment.Annotations = theme.Annotations
			deployment.Labels = theme.Labels
			if deployment.Labels == nil {
				deployment.Labels = make(map[string]string)
			}
			deployment.Labels["kdex.dev/theme"] = theme.Name
			deployment.Spec = appsv1.DeploymentSpec{
				Replicas: replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kdex.dev/theme": theme.Name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"kdex.dev/theme": theme.Name,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Env: []corev1.EnvVar{
									{
										Name: "POD_NAME",
										ValueFrom: &corev1.EnvVarSource{
											FieldRef: &corev1.ObjectFieldSelector{
												FieldPath: "metadata.name",
											},
										},
									},
									{
										Name:  "POD_NAMESPACE",
										Value: host.Namespace,
									},
									{
										Name: "POD_IP",
										ValueFrom: &corev1.EnvVarSource{
											FieldRef: &corev1.ObjectFieldSelector{
												FieldPath: "status.podIP",
											},
										},
									},
									{
										Name:  "CORS_DOMAINS",
										Value: utils.DomainsToMatcher(host.Spec.Routing.Domains),
									},
								},
								Image:           webserverImage,
								ImagePullPolicy: webserverPullPolicy,
								Name:            theme.Name,
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: defaults.Container.Port,
										Name:          theme.Name,
									},
								},
								Resources:       resources,
								SecurityContext: defaults.Container.SecurityContext.DeepCopy(),
								VolumeMounts: []corev1.VolumeMount{
									{
										MountPath: "/etc/caddy.d",
										Name:      "theme-scratch",
									},
									{
										MountPath: "/public",
										Name:      "theme-oci-image",
									},
								},
							},
						},
						ImagePullSecrets: theme.Spec.PullSecrets,
						SecurityContext:  defaults.Deployment.SecurityContext.DeepCopy(),
						Volumes: []corev1.Volume{
							{
								Name: "theme-scratch",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{
										Medium:    corev1.StorageMediumMemory,
										SizeLimit: &scratchSize,
									},
								},
							},
							{
								Name: "theme-oci-image",
								VolumeSource: corev1.VolumeSource{
									Image: &corev1.ImageVolumeSource{
										PullPolicy: themePullPolicy,
										Reference:  theme.Spec.Image,
									},
								},
							},
						},
					},
				},
			}

			return ctrl.SetControllerReference(host, deployment, r.Scheme)
		},
	); err != nil {
		log.Error(err, "unable to create or update Deployment", "deployment", deployment)
		return true, ctrl.Result{}, err
	}

	return false, ctrl.Result{}, nil
}

func (r *KDexHostReconciler) createOrUpdateThemeService(
	ctx context.Context,
	host *kdexv1alpha1.KDexHost,
	theme *kdexv1alpha1.KDexTheme,
) (bool, ctrl.Result, error) {
	log := logf.FromContext(ctx)

	defaults := r.Defaults

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      theme.Name,
			Namespace: host.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		service,
		func() error {
			service.Annotations = theme.Annotations
			service.Labels = theme.Labels
			if service.Labels == nil {
				service.Labels = make(map[string]string)
			}
			service.Labels["kdex.dev/theme"] = theme.Name
			service.Spec = corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       theme.Name,
						Port:       defaults.Container.Port,
						Protocol:   corev1.ProtocolTCP,
						TargetPort: intstr.FromInt32(defaults.Container.Port),
					},
				},
				Selector: map[string]string{
					"kdex.dev/theme": theme.Name,
				},
				Type: corev1.ServiceTypeClusterIP,
			}

			return ctrl.SetControllerReference(host, service, r.Scheme)
		},
	); err != nil {
		log.Error(err, "unable to create or update Service", "service", service.Name)
		return true, ctrl.Result{}, err
	}

	return false, ctrl.Result{}, nil
}

func (r *KDexHostReconciler) findHostsForTheme(
	ctx context.Context,
	theme client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var hostsList kdexv1alpha1.KDexHostList
	if err := r.List(ctx, &hostsList, &client.ListOptions{
		Namespace: theme.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexHost for theme", "name", theme.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(hostsList.Items))
	for _, host := range hostsList.Items {
		if host.Spec.DefaultThemeRef == nil {
			continue
		}
		if host.Spec.DefaultThemeRef.Name == theme.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      host.Name,
					Namespace: host.Namespace,
				},
			})
		}
	}
	return requests
}
