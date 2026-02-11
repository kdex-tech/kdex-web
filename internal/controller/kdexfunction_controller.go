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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	"kdex.dev/web/internal/build"
	"kdex.dev/web/internal/deploy"
	"kdex.dev/web/internal/generate"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KDexFunctionReconciler reconciles a KDexFunction object
type KDexFunctionReconciler struct {
	client.Client
	Configuration configuration.NexusConfiguration
	RequeueDelay  time.Duration
	Scheme        *runtime.Scheme
}

func (r *KDexFunctionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	var function kdexv1alpha1.KDexFunction
	if err := r.Get(ctx, req.NamespacedName, &function); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if function.Status.Attributes == nil {
		function.Status.Attributes = make(map[string]string)
	}

	// Defer status update
	defer func() {
		function.Status.ObservedGeneration = function.Generation
		if updateErr := r.Status().Update(ctx, &function); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}

		log.V(2).Info("status", "status", function.Status, "err", err, "res", res)
	}()

	kdexv1alpha1.SetConditions(
		&function.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		string(kdexv1alpha1.KDexFunctionStatePending),
	)

	host, shouldReturn, r1, err := ResolveHost(ctx, r.Client, &function, &function.Status.Conditions, &function.Spec.HostRef, r.RequeueDelay)
	if shouldReturn {
		return r1, err
	}

	faasAdaptorRef := host.Spec.FaaSAdaptorRef
	if faasAdaptorRef == nil {
		faasAdaptorRef = &kdexv1alpha1.KDexObjectReference{
			Kind: "KDexClusterFaaSAdaptor",
			Name: "kdex-default-faas-adaptor-knative",
		}
	}
	faasAdaptorObj, _, _, err := ResolveKDexObjectReference(ctx, r.Client, &function, &function.Status.Conditions, faasAdaptorRef, r.RequeueDelay)
	if err != nil {
		kdexv1alpha1.SetConditions(
			&function.Status.Conditions,
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

	var faasAdaptorSpec *kdexv1alpha1.KDexFaaSAdaptorSpec

	if faasAdaptorObj != nil {
		switch v := faasAdaptorObj.(type) {
		case *kdexv1alpha1.KDexClusterFaaSAdaptor:
			faasAdaptorSpec = &v.Spec
		case *kdexv1alpha1.KDexFaaSAdaptor:
			faasAdaptorSpec = &v.Spec
		}

		function.Status.Attributes["faasAdaptor.generation"] = fmt.Sprintf("%d", faasAdaptorObj.GetGeneration())
	}

	// Add generational awareness
	if function.Status.ObservedGeneration != function.Generation {
		function.Status.ObservedGeneration = function.Generation
		function.Status.GeneratorConfig = nil
		function.Status.Source = nil
		function.Status.Executable = ""
		function.Status.URL = ""
		function.Status.State = kdexv1alpha1.KDexFunctionStatePending
		function.Status.Detail = ""
		function.Status.Conditions = nil
	}

	// OpenAPIValid should result purely through validation webhook
	if !hasOpenAPISchemaURL(&function) {
		scheme := "http"
		if host.Spec.Routing.TLS != nil {
			scheme = "https"
		}
		function.Status.OpenAPISchemaURL = fmt.Sprintf("%s://%s/-/openapi?type=function&tag=%s", scheme, host.Spec.Routing.Domains[0], function.Name)
		if function.Status.Attributes == nil {
			function.Status.Attributes = make(map[string]string)
		}

		port := ""
		for _, p := range r.Configuration.HostDefault.Service.Ports {
			if p.Name == "server" {
				port = fmt.Sprintf(":%d", p.Port)
				break
			}
		}
		function.Status.Attributes["openapi.schema.url.internal"] = fmt.Sprintf("%s://%s/-/openapi?type=function&tag=%s", "http", host.Name+"."+host.Namespace+".svc.cluster.local"+port, function.Name)
		function.Status.State = kdexv1alpha1.KDexFunctionStateOpenAPIValid
		function.Status.Detail = "OpenAPISchemaURL:" + function.Status.OpenAPISchemaURL

		kdexv1alpha1.SetConditions(
			&function.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionFalse,
				Progressing: metav1.ConditionTrue,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconciling,
			string(kdexv1alpha1.KDexFunctionStateOpenAPIValid),
		)

		log.V(2).Info(string(kdexv1alpha1.KDexFunctionStateOpenAPIValid))

		return ctrl.Result{}, nil
	}

	// TODO: the Function.Language, Function.Environment and Function.Entrypoint should not need to be set by the developer.
	// If they are not set, they should be inferred from the FaaSAdaptorSpec.DefaultLanguage, FaaSAdaptorSpec.DefaultEnvironment and FaaSAdaptorSpec.DefaultEntrypoint.
	// If they are set, they should be validated here because we need the FaaSAdaptor to do that.

	// BuildValid can happen either manually by setting spec.function.generatorConfig
	if hasGeneratorConfig(&function) {
		function.Status.State = kdexv1alpha1.KDexFunctionStateBuildValid
		generatorConfig := function.Spec.Function.GeneratorConfig
		if generatorConfig == nil {
			generatorConfig = function.Status.GeneratorConfig
		}
		function.Status.Detail = "GeneratorImage:" + generatorConfig.Image
	} else if !hasSource(&function) && !hasExecutable(&function) && !hasURL(&function) {
		function.Status.GeneratorConfig = r.calculateGeneratorConfig(&function, faasAdaptorSpec)

		if function.Status.GeneratorConfig == nil {
			err := fmt.Errorf("GeneratorConfig %s/%s not found for function %s/%s", function.Spec.Function.Language, function.Spec.Function.Environment, function.Namespace, function.Name)
			kdexv1alpha1.SetConditions(
				&function.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)
			return ctrl.Result{}, err
		}

		function.Status.State = kdexv1alpha1.KDexFunctionStateBuildValid
		function.Status.Detail = "GeneratorImage:" + function.Status.GeneratorConfig.Image

		kdexv1alpha1.SetConditions(
			&function.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionFalse,
				Progressing: metav1.ConditionTrue,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconciling,
			string(kdexv1alpha1.KDexFunctionStateBuildValid),
		)

		log.V(2).Info(string(kdexv1alpha1.KDexFunctionStateBuildValid))

		return ctrl.Result{}, nil
	}

	if hasSource(&function) {
		function.Status.State = kdexv1alpha1.KDexFunctionStateSourceAvailable
		source := function.Spec.Function.Source
		if source == nil {
			source = function.Status.Source
		}
		function.Status.Detail = "Source: " + source.Repository + "/src/branch/" + source.Revision
	} else if hasGeneratorConfig(&function) && !hasExecutable(&function) && !hasURL(&function) {

		// The Builder will compute the Source which must be set in function.Status.Source and
		// function.Status.State = kdexv1alpha1.KDexFunctionStateSourceGenerated

		generatorConfig := function.Spec.Function.GeneratorConfig
		if generatorConfig == nil {
			generatorConfig = function.Status.GeneratorConfig
		}

		generator := generate.Generator{
			Client:          r.Client,
			Scheme:          r.Scheme,
			GeneratorConfig: *generatorConfig,
			ServiceAccount:  host.Name,
		}

		job, err := generator.GetOrCreateGenerateJob(ctx, &function)
		if err != nil {
			kdexv1alpha1.SetConditions(
				&function.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)
			return ctrl.Result{}, err
		}

		if job != nil {
			for _, cond := range job.Status.Conditions {
				if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
					err := fmt.Errorf("Code generation job %s/%s failed: %s", job.Namespace, job.Name, cond.Message)
					kdexv1alpha1.SetConditions(
						&function.Status.Conditions,
						kdexv1alpha1.ConditionStatuses{
							Degraded:    metav1.ConditionTrue,
							Progressing: metav1.ConditionFalse,
							Ready:       metav1.ConditionFalse,
						},
						kdexv1alpha1.ConditionReasonReconcileError,
						err.Error(),
					)
					return ctrl.Result{}, err
				}
			}

			kdexv1alpha1.SetConditions(
				&function.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionFalse,
					Progressing: metav1.ConditionTrue,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconciling,
				fmt.Sprintf("Waiting on code generation job %s/%s to complete", job.Namespace, job.Name),
			)

			log.V(2).Info(fmt.Sprintf("Waiting on code generation job %s/%s to complete", job.Namespace, job.Name))
		}

		return ctrl.Result{}, nil
	}

	if hasExecutable(&function) {
		function.Status.State = kdexv1alpha1.KDexFunctionStateExecutableAvailable
		executable := function.Spec.Function.Executable
		if executable == "" {
			executable = function.Status.Executable
		}
		function.Status.Detail = "Executable: " + executable
	} else if hasSource(&function) && !hasURL(&function) {

		// TODO: In this scenario we need to trigger creation of the function image.
		// The Builder will compute the image name and tag which must be set in function.Status.Executable and
		// function.Status.State = kdexv1alpha1.KDexFunctionStateExecutableAvailable

		source := function.Spec.Function.Source
		if source == nil {
			source = function.Status.Source
		}

		builder := build.Builder{
			Client:        r.Client,
			Scheme:        r.Scheme,
			Configuration: r.Configuration,
			Source:        *source,
		}

		op, imgUnstruct, err := builder.GetOrCreateKPackImage(ctx, &function)
		if err != nil {
			if strings.Contains(err.Error(), "Immutable field changed") {
				// we need to delete the image builder and try again because there was a change in our image repository
				// and the image builder is immutable.
				log.V(2).Info("Immutable field changed, deleting image builder", "image builder", imgUnstruct)
				err := r.Client.Delete(ctx, imgUnstruct)
				if err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}

			kdexv1alpha1.SetConditions(
				&function.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)
			return ctrl.Result{}, err
		}

		log.V(2).Info(
			"GetOrCreateKPackImage",
			"op", op,
			"generation", function.GetGeneration(),
			"source", source,
			"KPackImage", imgUnstruct,
		)

		if imgUnstruct != nil && imgUnstruct.Object != nil {
			status, ok := imgUnstruct.Object["status"].(map[string]any)
			if !ok {
				return ctrl.Result{}, fmt.Errorf("image builder job %s/%s has no status", imgUnstruct.GetNamespace(), imgUnstruct.GetName())
			}
			conditions, ok := status["conditions"].([]any)
			if !ok {
				return ctrl.Result{}, fmt.Errorf("image builder job %s/%s has no conditions", imgUnstruct.GetNamespace(), imgUnstruct.GetName())
			}
			for _, cond := range conditions {
				if cond.(map[string]any)["type"] == "Failed" && cond.(map[string]any)["status"] == "True" {
					err := fmt.Errorf("image builder job %s/%s failed: %s", imgUnstruct.GetNamespace(), imgUnstruct.GetName(), cond.(map[string]any)["message"])
					kdexv1alpha1.SetConditions(
						&function.Status.Conditions,
						kdexv1alpha1.ConditionStatuses{
							Degraded:    metav1.ConditionTrue,
							Progressing: metav1.ConditionFalse,
							Ready:       metav1.ConditionFalse,
						},
						kdexv1alpha1.ConditionReasonReconcileError,
						err.Error(),
					)
					return ctrl.Result{}, err
				} else if cond.(map[string]any)["type"] == "Ready" && cond.(map[string]any)["status"] == "True" {
					function.Status.State = kdexv1alpha1.KDexFunctionStateExecutableAvailable
					function.Status.Executable = status["latestImage"].(string)
					function.Status.Detail = "Image:" + function.Status.Executable
					tags := []string{}
					tag, ok, _ := unstructured.NestedString(imgUnstruct.Object, "spec", "tag")
					if ok {
						tags = append(tags, tag)
					}
					additionalTags, ok, _ := unstructured.NestedSlice(imgUnstruct.Object, "spec", "additionalTags")
					if ok {
						for _, t := range additionalTags {
							tags = append(tags, t.(string))
						}
					}
					function.Status.Attributes["image.tags"] = strings.Join(tags, ",")
				}
			}
		}

		if function.Status.State != kdexv1alpha1.KDexFunctionStateExecutableAvailable {
			return ctrl.Result{}, nil
		}

		kdexv1alpha1.SetConditions(
			&function.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionFalse,
				Progressing: metav1.ConditionTrue,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconciling,
			string(kdexv1alpha1.KDexFunctionStateExecutableAvailable),
		)

		log.V(2).Info(string(kdexv1alpha1.KDexFunctionStateExecutableAvailable))

		return ctrl.Result{}, nil
	}

	if hasURL(&function) {
		function.Status.State = kdexv1alpha1.KDexFunctionStateReady
		function.Status.Detail = "FunctionURL: " + function.Status.URL
	} else if hasExecutable(&function) {

		// TODO: In this scenario we need to trigger the function deployment and
		// wait for it to reconcile, then set the URL on function.Status.URL and
		// function.Status.State = kdexv1alpha1.KDexFunctionStateFunctionDeployed

		deployer := deploy.Deployer{
			Client:         r.Client,
			Scheme:         r.Scheme,
			Configuration:  r.Configuration,
			ServiceAccount: host.Name,
		}

		job, err := deployer.GetOrCreateDeployJob(ctx, &function, faasAdaptorSpec)
		if err != nil {
			kdexv1alpha1.SetConditions(
				&function.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				err.Error(),
			)
			return ctrl.Result{}, err
		}

		if job != nil {
			for _, cond := range job.Status.Conditions {
				if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
					err := fmt.Errorf("Function deployer job %s/%s failed: %s", job.Namespace, job.Name, cond.Message)
					kdexv1alpha1.SetConditions(
						&function.Status.Conditions,
						kdexv1alpha1.ConditionStatuses{
							Degraded:    metav1.ConditionTrue,
							Progressing: metav1.ConditionFalse,
							Ready:       metav1.ConditionFalse,
						},
						kdexv1alpha1.ConditionReasonReconcileError,
						err.Error(),
					)
					return ctrl.Result{}, err
				}
			}

			kdexv1alpha1.SetConditions(
				&function.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionFalse,
					Progressing: metav1.ConditionTrue,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconciling,
				fmt.Sprintf("Waiting on function deployer job %s/%s to complete", job.Namespace, job.Name),
			)

			log.V(2).Info(fmt.Sprintf("Waiting on function deployer job %s/%s to complete", job.Namespace, job.Name))
		}

		return ctrl.Result{}, nil
	}

	kdexv1alpha1.SetConditions(
		&function.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionFalse,
			Ready:       metav1.ConditionTrue,
		},
		kdexv1alpha1.ConditionReasonReconcileSuccess,
		"Reconciliation successful",
	)

	log.V(1).Info("reconciled")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexFunctionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexFunction{}).
		Watches(
			&kdexv1alpha1.KDexInternalHost{},
			MakeHandlerByReferencePath(r.Client, r.Scheme, &kdexv1alpha1.KDexFunction{}, &kdexv1alpha1.KDexFunctionList{}, "{.Spec.HostRef}")).
		Watches(
			&unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "kpack.io/v1alpha2",
					"kind":       "Image",
				},
			},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				functionName := o.GetLabels()["kdex.dev/function"]
				if functionName == "" {
					return nil
				}
				return []reconcile.Request{
					{
						NamespacedName: client.ObjectKey{
							Namespace: o.GetNamespace(),
							Name:      functionName,
						},
					},
				}
			})).
		WithOptions(controller.TypedOptions[reconcile.Request]{
			LogConstructor: LogConstructor("kdexfunction", mgr),
		}).
		Named("kdexfunction").
		Complete(r)
}

func (r *KDexFunctionReconciler) calculateGeneratorConfig(function *kdexv1alpha1.KDexFunction, faasAdaptorSpec *kdexv1alpha1.KDexFaaSAdaptorSpec) *kdexv1alpha1.GeneratorConfig {
	if faasAdaptorSpec == nil {
		return nil
	}

	language := function.Spec.Function.Language
	environment := function.Spec.Function.Environment

	generatorConfig, ok := faasAdaptorSpec.Generators[language+"/"+environment]

	if !ok {
		return nil
	}

	return &generatorConfig
}

func hasGeneratorConfig(function *kdexv1alpha1.KDexFunction) bool {
	return function.Spec.Function.GeneratorConfig != nil || function.Status.GeneratorConfig != nil
}

func hasSource(function *kdexv1alpha1.KDexFunction) bool {
	return function.Spec.Function.Source != nil || function.Status.Source != nil
}

func hasExecutable(function *kdexv1alpha1.KDexFunction) bool {
	return function.Spec.Function.Executable != "" || function.Status.Executable != ""
}

func hasOpenAPISchemaURL(function *kdexv1alpha1.KDexFunction) bool {
	return function.Status.OpenAPISchemaURL != ""
}

func hasURL(function *kdexv1alpha1.KDexFunction) bool {
	return function.Status.URL != ""
}
