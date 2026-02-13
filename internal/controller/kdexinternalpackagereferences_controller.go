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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	kjob "kdex.dev/web/internal/job"
	"kdex.dev/web/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KDexInternalPackageReferencesReconciler reconciles a KDexInternalPackageReferences object
type KDexInternalPackageReferencesReconciler struct {
	client.Client
	Configuration       configuration.NexusConfiguration
	ControllerNamespace string
	FocalHost           string
	RequeueDelay        time.Duration
	Scheme              *runtime.Scheme
}

func (r *KDexInternalPackageReferencesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logf.FromContext(ctx)

	if req.Namespace != r.ControllerNamespace {
		log.V(1).Info("skipping reconcile", "namespace", req.Namespace, "controllerNamespace", r.ControllerNamespace)
		return ctrl.Result{}, nil
	}

	var internalPackageReferences kdexv1alpha1.KDexInternalPackageReferences
	if err := r.Get(ctx, req.NamespacedName, &internalPackageReferences); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if internalPackageReferences.Name != fmt.Sprintf("%s-packages", r.FocalHost) {
		log.V(1).Info("skipping reconcile", "host", internalPackageReferences.Name, "focalHost", r.FocalHost)
		return ctrl.Result{}, nil
	}

	if internalPackageReferences.Status.Attributes == nil {
		internalPackageReferences.Status.Attributes = make(map[string]string)
	}

	// Defer status update
	defer func() {
		internalPackageReferences.Status.ObservedGeneration = internalPackageReferences.Generation
		if updateErr := r.Status().Update(ctx, &internalPackageReferences); updateErr != nil {
			err = updateErr
			res = ctrl.Result{}
		}

		log.V(1).Info("status", "status", internalPackageReferences.Status, "err", err, "res", res)
	}()

	kdexv1alpha1.SetConditions(
		&internalPackageReferences.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		"Reconciling",
	)

	return r.createOrUpdateJob(ctx, r.Configuration.BackendDefault.ModulePath, &internalPackageReferences)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexInternalPackageReferencesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasFocalHost := func(o client.Object) bool {
		switch t := o.(type) {
		case *kdexv1alpha1.KDexInternalHost:
			return t.Name == r.FocalHost
		case *kdexv1alpha1.KDexInternalPackageReferences:
			return t.Name == fmt.Sprintf("%s-packages", r.FocalHost)
		case *kdexv1alpha1.KDexPageBinding:
			return t.Spec.HostRef.Name == r.FocalHost
		case *kdexv1alpha1.KDexInternalTranslation:
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
		For(&kdexv1alpha1.KDexInternalPackageReferences{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		WithEventFilter(enabledFilter).
		WithOptions(
			controller.TypedOptions[reconcile.Request]{
				LogConstructor: LogConstructor("kdexinternalpackagereferences", mgr),
			},
		).
		Named("kdexinternalpackagereferences").
		Complete(r)
}

func (r *KDexInternalPackageReferencesReconciler) createOrUpdateJob(
	ctx context.Context,
	modulePath string,
	internalPackageReferences *kdexv1alpha1.KDexInternalPackageReferences,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Check if the work is already done for this spec (Idempotency check)
	currentPackageRefs := fmt.Sprintf("%v", internalPackageReferences.Spec.PackageReferences)
	if internalPackageReferences.Status.Attributes["packageReferences"] == currentPackageRefs &&
		internalPackageReferences.Status.Attributes["image"] != "" &&
		internalPackageReferences.Status.Attributes["importmap"] != "" {

		kdexv1alpha1.SetConditions(
			&internalPackageReferences.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionFalse,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionTrue,
			},
			kdexv1alpha1.ConditionReasonReconcileSuccess,
			"Reconciliation successful, package image ready",
		)
		return ctrl.Result{}, nil
	}

	// 2. Work not done, ensure support resources are up to date
	_, configMap, err := r.createOrUpdateJobConfigMap(ctx, modulePath, internalPackageReferences)
	if err != nil {
		return ctrl.Result{}, err
	}

	_, secret, err := r.createOrUpdateJobSecret(ctx, internalPackageReferences)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 3. Reconcile the Job
	jobName := fmt.Sprintf("%s-job", internalPackageReferences.Name)
	job := &batchv1.Job{}
	err = r.Get(ctx, client.ObjectKey{Namespace: internalPackageReferences.Namespace, Name: jobName}, job)

	if err == nil {
		// Job exists. Check if it's for the current generation.
		if job.Annotations["kdex.dev/generation"] != fmt.Sprintf("%d", internalPackageReferences.Generation) {
			log.V(2).Info("deleting stale job", "job", job.Name, "currentGeneration", internalPackageReferences.Generation)
			if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		// Correct generation. Check status.
		if job.Status.Succeeded > 0 {
			// Harvest results from the pods
			pod, err := kjob.GetPodForJob(ctx, r.Client, job)
			if err != nil {
				kdexv1alpha1.SetConditions(
					&internalPackageReferences.Status.Conditions,
					kdexv1alpha1.ConditionStatuses{
						Degraded:    metav1.ConditionTrue,
						Progressing: metav1.ConditionFalse,
						Ready:       metav1.ConditionFalse,
					},
					kdexv1alpha1.ConditionReasonReconcileError,
					err.Error(),
				)

				if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
					return ctrl.Result{}, err
				}

				return ctrl.Result{Requeue: true}, nil
			}

			var imageDigest string
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if containerStatus.Name == "kaniko" && containerStatus.State.Terminated != nil {
					imageDigest = containerStatus.State.Terminated.Message
					break
				}
			}

			var importmap string
			for _, containerStatus := range pod.Status.InitContainerStatuses {
				if containerStatus.Name == "importmap-generator" && containerStatus.State.Terminated != nil {
					importmap = containerStatus.State.Terminated.Message
					break
				}
			}

			if imageDigest == "" || importmap == "" {
				// Job reported success but we can't find the outputs yet? Wait a bit.
				return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
			}

			internalPackageReferences.Status.Attributes["image"] = fmt.Sprintf(
				"%s/%s@%s", r.Configuration.DefaultImageRegistry.Host, internalPackageReferences.Name, imageDigest,
			)
			internalPackageReferences.Status.Attributes["importmap"] = importmap
			internalPackageReferences.Status.Attributes["packageReferences"] = currentPackageRefs

			kdexv1alpha1.SetConditions(
				&internalPackageReferences.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionFalse,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionTrue,
				},
				kdexv1alpha1.ConditionReasonReconcileSuccess,
				"Reconciliation successful, package image ready",
			)

			log.V(1).Info("results harvested successfully", "job", job.Name)
			return ctrl.Result{}, nil
		}

		// Check for failure
		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
				kdexv1alpha1.SetConditions(
					&internalPackageReferences.Status.Conditions,
					kdexv1alpha1.ConditionStatuses{
						Degraded:    metav1.ConditionTrue,
						Progressing: metav1.ConditionFalse,
						Ready:       metav1.ConditionFalse,
					},
					kdexv1alpha1.ConditionReasonReconcileError,
					fmt.Sprintf("Job failed: %s", condition.Message),
				)

				return ctrl.Result{}, nil
			}
		}

		// Job is still in progress
		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// 4. Job doesn't exist, create it for the current generation
	newJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: internalPackageReferences.Namespace,
		},
	}
	r.setupJob(internalPackageReferences, newJob, configMap, secret)

	if err := ctrl.SetControllerReference(internalPackageReferences, newJob, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, newJob); err != nil {
		kdexv1alpha1.SetConditions(
			&internalPackageReferences.Status.Conditions,
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

	log.V(1).Info("job created", "job", newJob.Name, "generation", internalPackageReferences.Generation)
	return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
}

func (r *KDexInternalPackageReferencesReconciler) setupJob(
	internalPackageReferences *kdexv1alpha1.KDexInternalPackageReferences,
	job *batchv1.Job,
	configMap *corev1.ConfigMap,
	npmrcSecret *corev1.Secret,
) {
	if job.CreationTimestamp.IsZero() {
		job.Annotations = make(map[string]string)
		maps.Copy(job.Annotations, internalPackageReferences.Annotations)
		job.Labels = make(map[string]string)
		maps.Copy(job.Labels, internalPackageReferences.Labels)

		job.Annotations["kdex.dev/packages"] = internalPackageReferences.Name

		volumes := []corev1.Volume{
			{
				Name: "workspace",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
			{
				Name: "build-scripts",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: configMap.Name,
						},
					},
				},
			},
		}

		volumeMounts := []corev1.VolumeMount{
			{
				Name:      "workspace",
				MountPath: "/workspace",
			},
			{
				Name:      "build-scripts",
				MountPath: "/scripts",
				ReadOnly:  true,
			},
		}

		if npmrcSecret != nil {
			volumes = append(volumes, corev1.Volume{
				Name: "npmrc",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: npmrcSecret.Name,
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "npmrc",
				MountPath: "/workspace/.npmrc",
				SubPath:   ".npmrc",
				ReadOnly:  true,
			})
		}

		job.Spec = batchv1.JobSpec{
			BackoffLimit: utils.Ptr[int32](3),
			Completions:  utils.Ptr[int32](1),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kdex.dev/packages": internalPackageReferences.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:         "kaniko",
							Image:        "gcr.io/kaniko-project/executor:latest",
							VolumeMounts: volumeMounts,
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "npm-build",
							Image: "node:25-alpine",
							Command: []string{
								"sh",
								"-c",
								`
							set -e

							cp /scripts/package.json /workspace/package.json
							
							echo "======== package.json ========="
							cat /workspace/package.json
							echo "==============================="
							
							cd /workspace
							npm install

							npx esbuild node_modules/**/*.js --allow-overwrite --outdir=node_modules --define:process.env.NODE_ENV=\"production\"
							`,
							},
							VolumeMounts: volumeMounts,
						},
						{
							Name:  "importmap-generator",
							Image: "node:25-alpine",
							Command: []string{
								"sh",
								"-c",
								`
							set -e
							
							cp /scripts/generate.js /workspace/generate.js
							
							cd /workspace
							node generate.js

							cat importmap.json > /dev/termination-log
							`,
							},
							VolumeMounts: volumeMounts,
						},
					},
					RestartPolicy: "Never",
					Volumes:       volumes,
				},
			},
		}
	}

	job.Annotations["kdex.dev/generation"] = fmt.Sprintf("%d", internalPackageReferences.Generation)

	imageRepo := r.Configuration.DefaultImageRegistry.Host
	imageTag := fmt.Sprintf("%d", internalPackageReferences.Generation)
	imageURL := fmt.Sprintf("%s/%s:%s", imageRepo, internalPackageReferences.Name, imageTag)

	kanikoArgs := []string{
		"--dockerfile=/scripts/Dockerfile",
		"--context=dir:///workspace",
		"--destination=" + imageURL,
		"--digest-file=/dev/termination-log",
	}

	if r.Configuration.DefaultImageRegistry.InSecure {
		kanikoArgs = append(kanikoArgs, "--insecure")
	}

	job.Spec.Template.Annotations["kdex.dev/generation"] = fmt.Sprintf("%d", internalPackageReferences.Generation)
	job.Spec.Template.Annotations["kdex.dev/confighash"] = generateHashOf(configMap.Data)
	job.Spec.Template.Spec.Containers[0].Args = kanikoArgs
}

func (r *KDexInternalPackageReferencesReconciler) createOrUpdateJobConfigMap(
	ctx context.Context,
	modulePath string,
	internalPackageReferences *kdexv1alpha1.KDexInternalPackageReferences,
) (controllerutil.OperationResult, *corev1.ConfigMap, error) {
	configmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-configmap", internalPackageReferences.Name),
			Namespace: internalPackageReferences.Namespace,
		},
	}

	log := logf.FromContext(ctx).WithName("createOrUpdateJobConfigMap").WithValues("configmap", configmap.Name)

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		configmap,
		func() error {
			if configmap.CreationTimestamp.IsZero() {
				configmap.Annotations = make(map[string]string)
				maps.Copy(configmap.Annotations, internalPackageReferences.Annotations)
				configmap.Labels = make(map[string]string)
				maps.Copy(configmap.Labels, internalPackageReferences.Labels)

				configmap.Labels["kdex.dev/packages"] = internalPackageReferences.Name
			}

			dockerfile := `
FROM scratch
COPY --chown=65532:65532 importmap.json importmap.json
COPY --chown=65532:65532 node_modules .
`
			generateJS := fmt.Sprintf(`import { Generator } from '@jspm/generator';
import fs from 'fs';

const generator = new Generator({
    defaultProvider: 'nodemodules',
    env: ['production', 'module', 'browser'],
    integrity: true,
});

try {
    const packageJSONStr = fs.readFileSync('package.json', 'utf8');
    const packageJSON = JSON.parse(packageJSONStr);
    
    for (const [key, value] of Object.entries(packageJSON.dependencies)) {
        await generator.install(key);
    }

    let importMap = JSON.stringify(generator.getMap(), null, 2)

    importMap = importMap.replaceAll(/\.\/node_modules/g, '%s')

    console.log('The import map is:', importMap);

    fs.writeFileSync('importmap.json', importMap);
} catch (err) {
    console.error('Error:', err);
    process.exit(1);
}
`, modulePath)

			var packageJSON strings.Builder
			packageJSON.WriteString(`{
  "name": "importmap",
  "type": "module",
  "devDependencies": {
    "@jspm/generator": "^2.7.6",
    "esbuild": "^0.27.0"
  },
  "dependencies": {`)
			for i, pkg := range internalPackageReferences.Spec.PackageReferences {
				if i > 0 {
					packageJSON.WriteString(",")
				}
				fmt.Fprintf(&packageJSON, "\n    \"%s\": \"%s\"", pkg.Name, pkg.Version)
			}
			packageJSON.WriteString("\n  }\n}")

			configmap.Data = map[string]string{
				"Dockerfile":   dockerfile,
				"generate.js":  generateJS,
				"package.json": packageJSON.String(),
			}

			return ctrl.SetControllerReference(internalPackageReferences, configmap, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&internalPackageReferences.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return controllerutil.OperationResultNone, nil, err
	}

	log.V(1).Info("reconciled", "as", op)

	return op, configmap, nil
}

func (r *KDexInternalPackageReferencesReconciler) createOrUpdateJobSecret(
	ctx context.Context,
	internalPackageReferences *kdexv1alpha1.KDexInternalPackageReferences,
) (controllerutil.OperationResult, *corev1.Secret, error) {
	if r.Configuration.DefaultNpmRegistry.Host == "" {
		return controllerutil.OperationResultNone, nil, nil
	}

	registryUrl, err := url.Parse(r.Configuration.DefaultNpmRegistry.GetAddress())
	if err != nil {
		kdexv1alpha1.SetConditions(
			&internalPackageReferences.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return controllerutil.OperationResultNone, nil, err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-secret", internalPackageReferences.Name),
			Namespace: internalPackageReferences.Namespace,
		},
	}

	log := logf.FromContext(ctx).WithName("createOrUpdateJobSecret").WithValues("secret", secret.Name)

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		secret,
		func() error {
			if secret.CreationTimestamp.IsZero() {
				secret.Annotations = make(map[string]string)
				maps.Copy(secret.Annotations, internalPackageReferences.Annotations)
				secret.Labels = make(map[string]string)
				maps.Copy(secret.Labels, internalPackageReferences.Labels)

				secret.Labels["kdex.dev/packages"] = internalPackageReferences.Name

				npmrcContent := fmt.Sprintf("registry=%s\n", r.Configuration.DefaultNpmRegistry.GetAddress())
				if r.Configuration.DefaultNpmRegistry.AuthData.Token != "" {
					npmrcContent += fmt.Sprintf("//%s/:_authToken=%s\n", registryUrl.Host, r.Configuration.DefaultNpmRegistry.AuthData.Token)
				} else if r.Configuration.DefaultNpmRegistry.AuthData.Username != "" && r.Configuration.DefaultNpmRegistry.AuthData.Password != "" {
					authStr := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", r.Configuration.DefaultNpmRegistry.AuthData.Username, r.Configuration.DefaultNpmRegistry.AuthData.Password)))
					npmrcContent += fmt.Sprintf("//%s/:_auth=%s\n", registryUrl.Host, authStr)
				}

				secret.StringData = map[string]string{
					".npmrc": npmrcContent,
				}
			}

			return ctrl.SetControllerReference(internalPackageReferences, secret, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&internalPackageReferences.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return controllerutil.OperationResultNone, nil, err
	}

	log.V(1).Info("reconciled", "as", op)

	return op, secret, nil
}

func generateHashOf(data map[string]string) string {
	keys := slices.Collect(maps.Keys(data))
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(data[k]))
	}

	return hex.EncodeToString(h.Sum(nil))
}
