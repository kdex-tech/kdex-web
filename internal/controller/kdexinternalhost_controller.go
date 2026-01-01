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
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	"kdex.dev/web/internal/host"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	cr_handler "sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	hostIndexKey = "spec.hostRef.name"
)

// KDexInternalHostReconciler reconciles a KDexInternalHost object
type KDexInternalHostReconciler struct {
	client.Client
	Configuration       configuration.NexusConfiguration
	ControllerNamespace string
	FocalHost           string
	HostHandler         *host.HostHandler
	Port                int32
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
	ServiceName         string

	mu                 sync.RWMutex
	memoizedDeployment *appsv1.DeploymentSpec
	memoizedIngress    *networkingv1.IngressSpec
	memoizedService    *corev1.ServiceSpec
}

// +kubebuilder:rbac:groups=apps,resources=deployments,                       verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,                          verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,   verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterscriptlibraries,    verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexclusterthemes,             verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalhosts,             verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalhosts/finalizers,  verbs=update
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalhosts/status,      verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalpackagereferences, verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternalpagebindings,      verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexinternaltranslations,      verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexscriptlibraries,           verbs=get;list;watch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexthemes,                    verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,            verbs=get;list;watch;create;update;patch;delete

// nolint:gocyclo
func (r *KDexInternalHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	if req.Namespace != r.ControllerNamespace {
		log.V(1).Info("skipping reconcile", "namespace", req.Namespace, "controllerNamespace", r.ControllerNamespace)
		return ctrl.Result{}, nil
	}

	if req.Name != r.FocalHost {
		log.V(1).Info("skipping reconcile", "name", req.Name, "focalHost", r.FocalHost)
		return ctrl.Result{}, nil
	}

	var internalHost kdexv1alpha1.KDexInternalHost
	if err := r.Get(ctx, req.NamespacedName, &internalHost); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if internalHost.Status.Attributes == nil {
		internalHost.Status.Attributes = make(map[string]string)
	}

	// Defer status update
	defer func() {
		internalHost.Status.ObservedGeneration = internalHost.Generation
		if updateErr := r.Status().Update(ctx, &internalHost); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}

		log.V(1).Info("status", "status", internalHost.Status, "err", err, "res", res)
	}()

	kdexv1alpha1.SetConditions(
		&internalHost.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		"Reconciling",
	)

	themeObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalHost, &internalHost.Status.Conditions, internalHost.Spec.ThemeRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	var scriptLibraries []kdexv1alpha1.KDexScriptLibrarySpec
	var themeAssets []kdexv1alpha1.Asset

	if themeObj != nil {
		internalHost.Status.Attributes["theme.generation"] = fmt.Sprintf("%d", themeObj.GetGeneration())

		var themeSpec *kdexv1alpha1.KDexThemeSpec
		switch v := themeObj.(type) {
		case *kdexv1alpha1.KDexTheme:
			themeSpec = &v.Spec
		case *kdexv1alpha1.KDexClusterTheme:
			themeSpec = &v.Spec
		}

		themeAssets = themeSpec.Assets

		themeScriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalHost, &internalHost.Status.Conditions, themeSpec.ScriptLibraryRef, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}

		if themeScriptLibraryObj != nil {
			internalHost.Status.Attributes["theme.scriptLibrary.generation"] = fmt.Sprintf("%d", themeScriptLibraryObj.GetGeneration())

			var scriptLibrary kdexv1alpha1.KDexScriptLibrarySpec

			switch v := themeScriptLibraryObj.(type) {
			case *kdexv1alpha1.KDexScriptLibrary:
				scriptLibrary = v.Spec
			case *kdexv1alpha1.KDexClusterScriptLibrary:
				scriptLibrary = v.Spec
			}

			scriptLibraries = append(scriptLibraries, scriptLibrary)
		}
	}

	scriptLibraryObj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalHost, &internalHost.Status.Conditions, internalHost.Spec.ScriptLibraryRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	if scriptLibraryObj != nil {
		internalHost.Status.Attributes["scriptLibrary.generation"] = fmt.Sprintf("%d", scriptLibraryObj.GetGeneration())

		var scriptLibrary kdexv1alpha1.KDexScriptLibrarySpec

		switch v := scriptLibraryObj.(type) {
		case *kdexv1alpha1.KDexScriptLibrary:
			scriptLibrary = v.Spec
		case *kdexv1alpha1.KDexClusterScriptLibrary:
			scriptLibrary = v.Spec
		}

		scriptLibraries = append(scriptLibraries, scriptLibrary)
	}

	requiredBackends := []resolvedBackend{}
	seenPaths := map[string]bool{}
	for _, ref := range internalHost.Spec.RequiredBackends {
		obj, shouldReturn, r1, err := ResolveKDexObjectReference(ctx, r.Client, &internalHost, &internalHost.Status.Conditions, &ref, r.RequeueDelay)
		if shouldReturn {
			return r1, err
		}
		if obj == nil {
			continue
		}

		var backendSpec kdexv1alpha1.Backend
		switch v := obj.(type) {
		case *kdexv1alpha1.KDexApp:
			backendSpec = v.Spec.Backend
		case *kdexv1alpha1.KDexClusterApp:
			backendSpec = v.Spec.Backend
		case *kdexv1alpha1.KDexScriptLibrary:
			backendSpec = v.Spec.Backend
		case *kdexv1alpha1.KDexClusterScriptLibrary:
			backendSpec = v.Spec.Backend
		case *kdexv1alpha1.KDexTheme:
			backendSpec = v.Spec.Backend
		case *kdexv1alpha1.KDexClusterTheme:
			backendSpec = v.Spec.Backend
		}

		if seenPaths[backendSpec.IngressPath] {
			return ctrl.Result{}, fmt.Errorf("duplicated path %s, paths must be unique across backends and pages", backendSpec.IngressPath)
		}
		seenPaths[backendSpec.IngressPath] = true

		requiredBackends = append(requiredBackends, resolvedBackend{
			Backend:   backendSpec,
			Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		})
	}

	for _, pageHandler := range r.HostHandler.Pages.List() {
		if seenPaths[pageHandler.Page.BasePath] {
			return ctrl.Result{}, fmt.Errorf("duplicated path %s, paths must be unique across backends and pages", pageHandler.Page.BasePath)
		}
		seenPaths[pageHandler.Page.BasePath] = true
	}

	allPackageReferences := []kdexv1alpha1.PackageReference{}
	for _, scriptLibrary := range scriptLibraries {
		if scriptLibrary.PackageReference != nil {
			allPackageReferences = append(allPackageReferences, *scriptLibrary.PackageReference)
		}
	}

	for _, p := range r.HostHandler.Pages.List() {
		allPackageReferences = append(allPackageReferences, p.PackageReferences...)
	}

	uniquePackageReferences := map[string]kdexv1alpha1.PackageReference{}
	for _, pkgRef := range allPackageReferences {
		uniquePackageReferences[pkgRef.Name+"@"+pkgRef.Version] = pkgRef
	}

	finalPackageReferences := []kdexv1alpha1.PackageReference{}
	for _, pkgRef := range uniquePackageReferences {
		finalPackageReferences = append(finalPackageReferences, pkgRef)
	}

	internalPackageReferences := &kdexv1alpha1.KDexInternalPackageReferences{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-packages", internalHost.Name),
			Namespace: internalHost.Namespace,
		},
	}

	var importmap string

	if len(finalPackageReferences) == 0 {
		log.V(2).Info("deleting host package references", "packageReferences", internalPackageReferences.Name)

		if err := r.Delete(ctx, internalPackageReferences); err != nil {
			if client.IgnoreNotFound(err) != nil {
				kdexv1alpha1.SetConditions(
					&internalHost.Status.Conditions,
					kdexv1alpha1.ConditionStatuses{
						Degraded:    metav1.ConditionTrue,
						Progressing: metav1.ConditionFalse,
						Ready:       metav1.ConditionFalse,
					},
					kdexv1alpha1.ConditionReasonReconcileSuccess,
					err.Error(),
				)

				log.V(2).Info("error deleting package references", "packageReferences", internalPackageReferences.Name, "err", err)

				return ctrl.Result{}, err
			}
		}

		internalPackageReferences = nil
	} else {
		shouldReturn, r1, err = r.createOrUpdatePackageReferences(ctx, &internalHost, internalPackageReferences, finalPackageReferences)
		if shouldReturn {
			log.V(2).Info("package references shouldReturn", "packageReferences", internalPackageReferences.Name, "result", r1, "err", err)

			return r1, err
		}

		importmap = internalPackageReferences.Status.Attributes["importmap"]
	}

	r.HostHandler.SetHost(&internalHost.Spec.KDexHostSpec, themeAssets, scriptLibraries, importmap)

	return ctrl.Result{}, r.innerReconcile(ctx, &internalHost, internalPackageReferences, requiredBackends)
}

type resolvedBackend struct {
	Backend   kdexv1alpha1.Backend
	Kind      string
	Name      string
	Namespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexInternalHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasFocalHost := func(o client.Object) bool {
		switch t := o.(type) {
		case *kdexv1alpha1.KDexInternalHost:
			return t.Name == r.FocalHost
		case *kdexv1alpha1.KDexInternalPackageReferences:
			return t.Name == fmt.Sprintf("%s-packages", r.FocalHost)
		case *kdexv1alpha1.KDexInternalPageBinding:
			return t.Spec.HostRef.Name == r.FocalHost
		case *kdexv1alpha1.KDexInternalTranslation:
			return t.Spec.HostRef.Name == r.FocalHost
		default:
			return true
		}
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &kdexv1alpha1.KDexInternalPageBinding{}, hostIndexKey, func(rawObj client.Object) []string {
		pageBinding := rawObj.(*kdexv1alpha1.KDexInternalPageBinding)
		if pageBinding.Spec.HostRef.Name == "" {
			return nil
		}
		return []string{pageBinding.Spec.HostRef.Name}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &kdexv1alpha1.KDexInternalTranslation{}, hostIndexKey, func(rawObj client.Object) []string {
		translation := rawObj.(*kdexv1alpha1.KDexInternalTranslation)
		if translation.Spec.HostRef.Name == "" {
			return nil
		}
		return []string{translation.Spec.HostRef.Name}
	}); err != nil {
		return err
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
		For(&kdexv1alpha1.KDexInternalHost{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Owns(&kdexv1alpha1.KDexInternalPackageReferences{}).
		Owns(&networkingv1.Ingress{}).
		Watches(
			&kdexv1alpha1.KDexScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalHost{}, &kdexv1alpha1.KDexInternalHostList{}, "{.Spec.Host.ScriptLibraryRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterScriptLibrary{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalHost{}, &kdexv1alpha1.KDexInternalHostList{}, "{.Spec.Host.ScriptLibraryRef}")).
		Watches(
			&kdexv1alpha1.KDexTheme{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalHost{}, &kdexv1alpha1.KDexInternalHostList{}, "{.Spec.Host.DefaultThemeRef}")).
		Watches(
			&kdexv1alpha1.KDexClusterTheme{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexInternalHost{}, &kdexv1alpha1.KDexInternalHostList{}, "{.Spec.Host.DefaultThemeRef}")).
		Watches(
			&kdexv1alpha1.KDexInternalPageBinding{},
			cr_handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				pageBinding, ok := obj.(*kdexv1alpha1.KDexInternalPageBinding)
				if !ok || pageBinding.Spec.HostRef.Name != r.FocalHost {
					return nil
				}

				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      pageBinding.Spec.HostRef.Name,
							Namespace: pageBinding.Namespace,
						},
					},
				}
			})).
		Watches(
			&kdexv1alpha1.KDexInternalTranslation{},
			cr_handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				translation, ok := o.(*kdexv1alpha1.KDexInternalTranslation)
				if !ok || translation.Spec.HostRef.Name != r.FocalHost {
					return nil
				}

				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      translation.Spec.HostRef.Name,
							Namespace: translation.Namespace,
						},
					},
				}
			})).
		WithEventFilter(enabledFilter).
		WithOptions(
			controller.TypedOptions[reconcile.Request]{
				LogConstructor: LogConstructor("kdexinternalhost", mgr),
			},
		).
		Named("kdexinternalhost").
		Complete(r)
}

func (r *KDexInternalHostReconciler) getMemoizedBackendDeployment() *appsv1.DeploymentSpec {
	r.mu.RLock()

	if r.memoizedDeployment != nil {
		r.mu.RUnlock()
		return r.memoizedDeployment
	}

	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.memoizedDeployment = r.Configuration.BackendDefault.Deployment.DeepCopy()

	return r.memoizedDeployment
}

func (r *KDexInternalHostReconciler) getMemoizedIngress() *networkingv1.IngressSpec {
	r.mu.RLock()

	if r.memoizedIngress != nil {
		r.mu.RUnlock()
		return r.memoizedIngress
	}

	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.memoizedIngress = r.Configuration.BackendDefault.Ingress.DeepCopy()

	return r.memoizedIngress
}

func (r *KDexInternalHostReconciler) getMemoizedService() *corev1.ServiceSpec {
	r.mu.RLock()

	if r.memoizedService != nil {
		r.mu.RUnlock()
		return r.memoizedService
	}

	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	r.memoizedService = r.Configuration.BackendDefault.Service.DeepCopy()

	return r.memoizedService
}

func (r *KDexInternalHostReconciler) innerReconcile(
	ctx context.Context,
	internalHost *kdexv1alpha1.KDexInternalHost,
	internalPackageReferences *kdexv1alpha1.KDexInternalPackageReferences,
	backends []resolvedBackend,
) error {
	// Synthetic Backend for the packages
	packagesBackend := resolvedBackend{
		Backend: kdexv1alpha1.Backend{
			IngressPath:           r.Configuration.BackendDefault.ModulePath,
			ServerImage:           internalPackageReferences.Status.Attributes["image"],
			ServerImagePullPolicy: corev1.PullIfNotPresent,
		},
		Name: internalPackageReferences.Name,
		Kind: "KDexInternalPackageReferences",
	}

	var err error

	backends = append(backends, packagesBackend)
	backendOps := map[string]controllerutil.OperationResult{}

	for _, rb := range backends {
		keyBase := fmt.Sprintf("%s/%s", strings.ToLower(rb.Kind), rb.Name)

		backendOps[keyBase+"/deployment"], err = r.createOrUpdateBackendDeployment(ctx, internalHost, rb.Name, rb.Kind, rb.Backend)
		if err != nil {
			return err
		}
		backendOps[keyBase+"/service"], err = r.createOrUpdateBackendService(ctx, internalHost, rb.Name, rb.Kind)
		if err != nil {
			return err
		}
	}

	if err := r.cleanupObsoleteBackends(ctx, internalHost, backends); err != nil {
		return err
	}

	var ingressOrHTTPRouteOp controllerutil.OperationResult
	if internalHost.Spec.Routing.Strategy == kdexv1alpha1.HTTPRouteRoutingStrategy {
		ingressOrHTTPRouteOp, err = r.createOrUpdateHTTPRoute(ctx, internalHost, backends)
		if err != nil {
			return err
		}
	} else {
		ingressOrHTTPRouteOp, err = r.createOrUpdateIngress(ctx, internalHost, backends)
		if err != nil {
			return err
		}
	}

	kdexv1alpha1.SetConditions(
		&internalHost.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	log := logf.FromContext(ctx)

	log.V(1).Info(
		"reconciled",
		"backendOps", backendOps,
		"ingressOrHTTPRouteOp", ingressOrHTTPRouteOp,
	)

	return nil
}

func (r *KDexInternalHostReconciler) createOrUpdatePackageReferences(
	ctx context.Context,
	internalHost *kdexv1alpha1.KDexInternalHost,
	internalPackageReferences *kdexv1alpha1.KDexInternalPackageReferences,
	packageReferences []kdexv1alpha1.PackageReference,
) (bool, ctrl.Result, error) {
	log := logf.FromContext(ctx)

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		internalPackageReferences,
		func() error {
			if internalPackageReferences.CreationTimestamp.IsZero() {
				internalPackageReferences.Annotations = make(map[string]string)
				for key, value := range internalHost.Annotations {
					internalPackageReferences.Annotations[key] = value
				}
				internalPackageReferences.Labels = make(map[string]string)
				for key, value := range internalHost.Labels {
					internalPackageReferences.Labels[key] = value
				}

				internalPackageReferences.Labels["kdex.dev/packages"] = internalPackageReferences.Name
			}

			internalPackageReferences.Spec.PackageReferences = packageReferences

			return ctrl.SetControllerReference(internalHost, internalPackageReferences, r.Scheme)
		},
	)

	log.V(2).Info(
		"createOrUpdatePackageReferences",
		"op", op,
		"attributes", internalPackageReferences.Status.Attributes,
		"generation", internalPackageReferences.Generation,
		"observedGeneration", internalPackageReferences.Status.ObservedGeneration,
		"packageReferences", internalPackageReferences.Spec.PackageReferences,
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&internalHost.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			err.Error(),
		)

		return true, ctrl.Result{}, err
	}

	if internalPackageReferences.Status.Attributes["image"] == "" ||
		internalPackageReferences.Status.Attributes["importmap"] == "" {

		kdexv1alpha1.SetConditions(
			&internalHost.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionFalse,
				Progressing: metav1.ConditionTrue,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			"image not available yet, requeueing",
		)

		return true, ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	return false, ctrl.Result{}, nil
}

func (r *KDexInternalHostReconciler) createOrUpdateIngress(
	ctx context.Context,
	internalHost *kdexv1alpha1.KDexInternalHost,
	backends []resolvedBackend,
) (controllerutil.OperationResult, error) {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internalHost.Name,
			Namespace: internalHost.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		ingress,
		func() error {
			if ingress.CreationTimestamp.IsZero() {
				ingress.Annotations = make(map[string]string)
				for key, value := range internalHost.Annotations {
					ingress.Annotations[key] = value
				}
				ingress.Labels = make(map[string]string)
				for key, value := range internalHost.Labels {
					ingress.Labels[key] = value
				}

				ingress.Labels["kdex.dev/ingress"] = ingress.Name

				ingress.Spec = *r.getMemoizedIngress().DeepCopy()

				if ingress.Spec.DefaultBackend == nil {
					ingress.Spec.DefaultBackend = &networkingv1.IngressBackend{}
				}

				if ingress.Spec.DefaultBackend.Service == nil {
					ingress.Spec.DefaultBackend.Service = &networkingv1.IngressServiceBackend{}
				}

				ingress.Spec.DefaultBackend.Service.Name = r.ServiceName

				ingress.Spec.DefaultBackend.Service.Port.Name = internalHost.Name
				ingress.Spec.IngressClassName = internalHost.Spec.Routing.IngressClassName
			}

			pathType := networkingv1.PathTypePrefix
			rules := make([]networkingv1.IngressRule, 0, len(internalHost.Spec.Routing.Domains))

			for _, domain := range internalHost.Spec.Routing.Domains {
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

			for _, rb := range backends {
				for _, rule := range rules {
					rule.HTTP.Paths = append(rule.HTTP.Paths,
						networkingv1.HTTPIngressPath{
							Path:     rb.Backend.IngressPath,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: rb.Name,
									Port: networkingv1.ServiceBackendPort{
										Name: "server",
									},
								},
							},
						},
					)
				}
			}

			ingress.Spec.Rules = append(r.getMemoizedIngress().Rules, rules...)

			if internalHost.Spec.Routing.TLS != nil {
				ingress.Spec.TLS = append(ingress.Spec.TLS, networkingv1.IngressTLS{
					SecretName: internalHost.Spec.Routing.TLS.SecretRef.Name,
				})
			}

			return ctrl.SetControllerReference(internalHost, ingress, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&internalHost.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return controllerutil.OperationResultNone, err
	}

	return op, nil
}

func (r *KDexInternalHostReconciler) createOrUpdateHTTPRoute(
	_ context.Context,
	_ *kdexv1alpha1.KDexInternalHost,
	_ []resolvedBackend,
) (controllerutil.OperationResult, error) {
	return controllerutil.OperationResultNone, nil
}

func (r *KDexInternalHostReconciler) createOrUpdateBackendDeployment(
	ctx context.Context,
	internalHost *kdexv1alpha1.KDexInternalHost,
	name string,
	kind string,
	backend kdexv1alpha1.Backend,
) (controllerutil.OperationResult, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: internalHost.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		deployment,
		func() error {
			if deployment.CreationTimestamp.IsZero() {
				deployment.Annotations = make(map[string]string)
				for key, value := range internalHost.Annotations {
					deployment.Annotations[key] = value
				}
				deployment.Labels = make(map[string]string)
				for key, value := range internalHost.Labels {
					deployment.Labels[key] = value
				}

				deployment.Labels["kdex.dev/type"] = "backend"
				deployment.Labels["kdex.dev/backend"] = name
				deployment.Labels["kdex.dev/host"] = internalHost.Name
				deployment.Labels["kdex.dev/kind"] = kind

				deployment.Spec = *r.getMemoizedBackendDeployment().DeepCopy()

				deployment.Spec.Selector.MatchLabels["kdex.dev/type"] = "backend"
				deployment.Spec.Selector.MatchLabels["kdex.dev/backend"] = name
				deployment.Spec.Selector.MatchLabels["kdex.dev/host"] = internalHost.Name
				deployment.Spec.Selector.MatchLabels["kdex.dev/kind"] = kind

				deployment.Spec.Template.Labels["kdex.dev/type"] = "backend"
				deployment.Spec.Template.Labels["kdex.dev/backend"] = name
				deployment.Spec.Template.Labels["kdex.dev/host"] = internalHost.Name
				deployment.Spec.Template.Labels["kdex.dev/kind"] = kind
			}

			deployment.Spec.Template.Spec.Containers[0].Name = name

			if len(backend.ImagePullSecrets) > 0 {
				deployment.Spec.Template.Spec.ImagePullSecrets = append(r.getMemoizedBackendDeployment().Template.Spec.ImagePullSecrets, backend.ImagePullSecrets...)
			}

			if backend.Replicas != nil {
				deployment.Spec.Replicas = backend.Replicas
			}

			if backend.Resources.Size() > 0 {
				deployment.Spec.Template.Spec.Containers[0].Resources = backend.Resources
			}

			if backend.ServerImage != "" {
				deployment.Spec.Template.Spec.Containers[0].Image = backend.ServerImage
			} else {
				deployment.Spec.Template.Spec.Containers[0].Image = r.Configuration.BackendDefault.ServerImage
			}

			if backend.ServerImagePullPolicy != "" {
				deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = backend.ServerImagePullPolicy
			} else {
				deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = r.Configuration.BackendDefault.ServerImagePullPolicy
			}

			if backend.StaticImage != "" {
				foundOCIVolume := false
				for idx, value := range deployment.Spec.Template.Spec.Volumes {
					if value.Name == "oci-image" {
						foundOCIVolume = true
						deployment.Spec.Template.Spec.Volumes[idx].Image.Reference = backend.StaticImage
						deployment.Spec.Template.Spec.Volumes[idx].Image.PullPolicy = backend.StaticImagePullPolicy
						break
					}
				}

				if !foundOCIVolume {
					deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
						Name: "oci-image",
						VolumeSource: corev1.VolumeSource{
							Image: &corev1.ImageVolumeSource{
								Reference:  backend.StaticImage,
								PullPolicy: backend.StaticImagePullPolicy,
							},
						},
					})
				}

				foundOCIVolumeMount := false
				for idx, value := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
					if value.Name == "oci-image" {
						foundOCIVolumeMount = true
						deployment.Spec.Template.Spec.Containers[0].VolumeMounts[idx].MountPath = "/public"
						deployment.Spec.Template.Spec.Containers[0].VolumeMounts[idx].Name = "oci-image"
						deployment.Spec.Template.Spec.Containers[0].VolumeMounts[idx].ReadOnly = true
						break
					}
				}

				if !foundOCIVolumeMount {
					deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
						Name:      "oci-image",
						MountPath: "/public",
						ReadOnly:  true,
					})
				}
			}

			return ctrl.SetControllerReference(internalHost, deployment, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&internalHost.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return controllerutil.OperationResultNone, err
	}

	return op, nil
}

func (r *KDexInternalHostReconciler) createOrUpdateBackendService(
	ctx context.Context,
	internalHost *kdexv1alpha1.KDexInternalHost,
	name string,
	kind string,
) (controllerutil.OperationResult, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: internalHost.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		service,
		func() error {
			if service.CreationTimestamp.IsZero() {
				service.Annotations = make(map[string]string)
				for key, value := range internalHost.Annotations {
					service.Annotations[key] = value
				}
				service.Labels = make(map[string]string)
				for key, value := range internalHost.Labels {
					service.Labels[key] = value
				}

				service.Labels["kdex.dev/type"] = "backend"
				service.Labels["kdex.dev/backend"] = name
				service.Labels["kdex.dev/host"] = internalHost.Name
				service.Labels["kdex.dev/kind"] = kind

				service.Spec = *r.getMemoizedService().DeepCopy()

				service.Spec.Selector = make(map[string]string)

				service.Spec.Selector["kdex.dev/type"] = "backend"
				service.Spec.Selector["kdex.dev/backend"] = name
				service.Spec.Selector["kdex.dev/host"] = internalHost.Name
				service.Spec.Selector["kdex.dev/kind"] = kind
			}

			return ctrl.SetControllerReference(internalHost, service, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&internalHost.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return controllerutil.OperationResultNone, err
	}

	return op, nil
}

func (r *KDexInternalHostReconciler) cleanupObsoleteBackends(
	ctx context.Context,
	internalHost *kdexv1alpha1.KDexInternalHost,
	backends []resolvedBackend,
) error {
	backendNames := make(map[string]bool)
	for _, rb := range backends {
		backendNames[rb.Name] = true
	}

	labelSelector := client.MatchingLabels{
		"kdex.dev/type": "backend",
		"kdex.dev/host": internalHost.Name,
	}

	// Cleanup Deployments
	deploymentList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deploymentList, client.InNamespace(internalHost.Namespace), labelSelector); err != nil {
		return err
	}

	for _, deployment := range deploymentList.Items {
		if !backendNames[deployment.Name] {
			if err := r.Delete(ctx, &deployment); err != nil {
				return err
			}
		}
	}

	// Cleanup Services
	serviceList := &corev1.ServiceList{}
	if err := r.List(ctx, serviceList, client.InNamespace(internalHost.Namespace), labelSelector); err != nil {
		return err
	}

	for _, service := range serviceList.Items {
		if !backendNames[service.Name] {
			if err := r.Delete(ctx, &service); err != nil {
				return err
			}
		}
	}

	return nil
}
