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
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// KDexHostPackageReferencesReconciler reconciles a KDexHostPackageReferences object
type KDexHostPackageReferencesReconciler struct {
	client.Client
	Configuration configuration.NexusConfiguration
	RequeueDelay  time.Duration
	Scheme        *runtime.Scheme
}

// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhostpackagereferences,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhostpackagereferences/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kdex.dev,resources=kdexhostpackagereferences/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *KDexHostPackageReferencesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var hostPackageReferences kdexv1alpha1.KDexHostPackageReferences
	if err := r.Get(ctx, req.NamespacedName, &hostPackageReferences); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Defer status update
	defer func() {
		hostPackageReferences.Status.ObservedGeneration = hostPackageReferences.Generation
		if updateErr := r.Status().Update(ctx, &hostPackageReferences); updateErr != nil {
			if res == (ctrl.Result{}) {
				err = updateErr
			}
		}
	}()

	if meta.IsStatusConditionTrue(hostPackageReferences.Status.Conditions, string(kdexv1alpha1.ConditionTypeReady)) &&
		hostPackageReferences.Generation == hostPackageReferences.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	kdexv1alpha1.SetConditions(
		&hostPackageReferences.Status.Conditions,
		kdexv1alpha1.ConditionStatuses{
			Degraded:    metav1.ConditionFalse,
			Progressing: metav1.ConditionTrue,
			Ready:       metav1.ConditionUnknown,
		},
		kdexv1alpha1.ConditionReasonReconciling,
		"Reconciling",
	)

	return r.createOrUpdateJob(ctx, &hostPackageReferences)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KDexHostPackageReferencesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kdexv1alpha1.KDexHostPackageReferences{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Watches(
			&batchv1.Job{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &kdexv1alpha1.KDexHostPackageReferences{}, handler.OnlyControllerOwner()),
		).
		Named("kdexhostpackagereferences").
		Complete(r)
}

func (r *KDexHostPackageReferencesReconciler) createOrUpdateJob(
	ctx context.Context,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) (ctrl.Result, error) {
	configMap, err := r.createOrUpdateJobConfigMap(ctx, hostPackageReferences)
	if err != nil {
		return ctrl.Result{}, err
	}

	secret, err := r.createOrUpdateJobSecret(ctx, hostPackageReferences)
	if err != nil {
		return ctrl.Result{}, err
	}

	log := ctrl.Log.WithName("createOrUpdateJob")

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-job", hostPackageReferences.Name),
			Namespace: hostPackageReferences.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		job,
		func() error {
			if job.ObjectMeta.CreationTimestamp.IsZero() {
				r.setupJob(hostPackageReferences, job, configMap, secret)
			}

			return ctrl.SetControllerReference(hostPackageReferences, job, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&hostPackageReferences.Status.Conditions,
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

	log.Info("job created", "job", job.Name, "operation", op)

	if job.Labels["kdex.dev/generation"] != fmt.Sprintf("%d", hostPackageReferences.Generation) {
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			log.Error(
				err, "failed to delete old job", "job", job.Name,
				"oldGeneration", job.Labels["kdex.dev/generation"],
				"newGeneration", hostPackageReferences.Generation,
			)
		}

		log.Info(
			"job generation does not match, deleting and requeueing",
			"job", job.Name,
			"oldGeneration", job.Labels["kdex.dev/generation"],
			"newGeneration", hostPackageReferences.Generation,
		)

		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	log.Info("job status", "job", job.Name, "status", job.Status)

	if job.Status.Succeeded > 0 {
		pod, err := r.getPodForJob(ctx, job)
		if err != nil {
			kdexv1alpha1.SetConditions(
				&hostPackageReferences.Status.Conditions,
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

		var imageDigest string
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == "kaniko" && containerStatus.State.Terminated != nil {
				imageDigest = containerStatus.State.Terminated.Message
				break
			}
		}

		if imageDigest == "" {
			return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
		}

		var importmap string
		for _, containerStatus := range pod.Status.InitContainerStatuses {
			if containerStatus.Name == "importmap-generator" && containerStatus.State.Terminated != nil {
				importmap = containerStatus.State.Terminated.Message
				break
			}
		}

		if importmap == "" {
			return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
		}

		imageRepo := r.Configuration.DefaultImageRegistry.Host
		imageURL := fmt.Sprintf("%s/%s@%s", imageRepo, hostPackageReferences.Name, imageDigest)
		hostPackageReferences.Status.Image = imageURL

		// TODO add field to CRD
		// hostPackageReferences.Status.ImportMap = importmap

		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			log.Error(err, "failed to delete successful job after completion", "job", job.Name)
		}
	} else if job.Status.Failed > 0 {
		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	} else {
		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

	kdexv1alpha1.SetConditions(
		&hostPackageReferences.Status.Conditions,
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

func (r *KDexHostPackageReferencesReconciler) getPodForJob(
	ctx context.Context,
	job *batchv1.Job,
) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found for job %s", job.Name)
	}

	return &podList.Items[0], nil
}

func (r *KDexHostPackageReferencesReconciler) setupJob(
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
	job *batchv1.Job,
	configMap *corev1.ConfigMap,
	npmrcSecret *corev1.Secret,
) {
	job.Annotations = make(map[string]string)
	for key, value := range hostPackageReferences.Annotations {
		job.Annotations[key] = value
	}
	job.Labels = make(map[string]string)
	for key, value := range hostPackageReferences.Labels {
		job.Labels[key] = value
	}

	job.Labels["kdex.dev/packages"] = hostPackageReferences.Name
	job.Labels["kdex.dev/generation"] = fmt.Sprintf("%d", hostPackageReferences.Generation)

	imageRepo := r.Configuration.DefaultImageRegistry.Host
	imageTag := fmt.Sprintf("%d", hostPackageReferences.Generation)
	imageURL := fmt.Sprintf("%s/%s:%s", imageRepo, hostPackageReferences.Name, imageTag)

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

	npmInstallVolumeMounts := []corev1.VolumeMount{
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
		npmInstallVolumeMounts = append(npmInstallVolumeMounts, corev1.VolumeMount{
			Name:      "npmrc",
			MountPath: "/workspace/.npmrc",
			SubPath:   ".npmrc",
			ReadOnly:  true,
		})
	}

	kanikoArgs := []string{
		"--dockerfile=/scripts/Dockerfile",
		"--context=dir:///workspace",
		"--destination=" + imageURL,
		"--digest-file=/dev/termination-log",
	}

	if r.Configuration.DefaultImageRegistry.InSecure {
		kanikoArgs = append(kanikoArgs, "--insecure")
	}

	backoffLimit := int32(4)

	job.Spec = batchv1.JobSpec{
		BackoffLimit: &backoffLimit,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "kaniko",
						Image: "gcr.io/kaniko-project/executor:latest",
						Args:  kanikoArgs,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "build-scripts",
								MountPath: "/scripts",
								ReadOnly:  true,
							},
							{
								Name:      "workspace",
								MountPath: "/workspace",
								ReadOnly:  true,
							},
						},
					},
				},
				InitContainers: []corev1.Container{
					{
						Name:  "importmap-generator",
						Image: "node:25-alpine",
						Command: []string{
							"sh",
							"-c",
							`
							set -e
							
							cp /scripts/package.json /workspace/package.json
							cd /workspace
							npm install
							
							cp /scripts/generate.js /workspace/generate.js
							node generate.js
							cat importmap.json > /dev/termination-log
							`,
						},
						VolumeMounts: npmInstallVolumeMounts,
					},
				},
				RestartPolicy: "Never",
				Volumes:       volumes,
			},
		},
	}
}

func (r *KDexHostPackageReferencesReconciler) createOrUpdateJobConfigMap(
	ctx context.Context,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) (*corev1.ConfigMap, error) {
	log := ctrl.Log.WithName("createOrUpdateJobConfigMap")

	configmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-configmap", hostPackageReferences.Name),
			Namespace: hostPackageReferences.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		configmap,
		func() error {
			if configmap.ObjectMeta.CreationTimestamp.IsZero() {
				configmap.Annotations = make(map[string]string)
				for key, value := range hostPackageReferences.Annotations {
					configmap.Annotations[key] = value
				}
				configmap.Labels = make(map[string]string)
				for key, value := range hostPackageReferences.Labels {
					configmap.Labels[key] = value
				}

				configmap.Labels["kdex.dev/packages"] = hostPackageReferences.Name
			}

			dockerfile := `
FROM scratch
COPY importmap.json /modules/importmap.json
COPY node_modules /modules
`
			generateJS := `import { Generator } from "@jspm/generator";
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

    importMap = importMap.replaceAll(/\.\/node_modules/g, '/modules')

    console.error('The import map is:', importMap);

    fs.writeFileSync('importmap.json', importMap);
} catch (err) {
    console.error('Error:', err);
}
`

			packageJSON := `{
  "name": "importmap",
  "type": "module",
  "devDependencies": {
    "@jspm/generator": "^2.7.6"
  },
  "dependencies": {`
			for i, pkg := range hostPackageReferences.Spec.PackageReferences {
				if i > 0 {
					packageJSON += ","
				}
				packageJSON += fmt.Sprintf(`"%s": "%s"`, pkg.Name, pkg.Version)
			}
			packageJSON += `}}`

			configmap.Data = map[string]string{
				"Dockerfile":   dockerfile,
				"generate.js":  generateJS,
				"package.json": packageJSON,
			}

			return ctrl.SetControllerReference(hostPackageReferences, configmap, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&hostPackageReferences.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return nil, err
	}

	log.Info("configmap created", "configmap", configmap.Name, "operation", op)

	return configmap, nil
}

func (r *KDexHostPackageReferencesReconciler) createOrUpdateJobSecret(
	ctx context.Context,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) (*corev1.Secret, error) {
	if r.Configuration.DefaultNpmRegistry.Host == "" {
		return nil, nil
	}

	registryUrl, err := url.Parse(r.Configuration.DefaultNpmRegistry.GetAddress())
	if err != nil {
		kdexv1alpha1.SetConditions(
			&hostPackageReferences.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return nil, err
	}

	log := ctrl.Log.WithName("createOrUpdateJobSecret")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-secret", hostPackageReferences.Name),
			Namespace: hostPackageReferences.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		secret,
		func() error {
			if secret.ObjectMeta.CreationTimestamp.IsZero() {
				secret.Annotations = make(map[string]string)
				for key, value := range hostPackageReferences.Annotations {
					secret.Annotations[key] = value
				}
				secret.Labels = make(map[string]string)
				for key, value := range hostPackageReferences.Labels {
					secret.Labels[key] = value
				}

				secret.Labels["kdex.dev/packages"] = hostPackageReferences.Name

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

			return ctrl.SetControllerReference(hostPackageReferences, secret, r.Scheme)
		},
	)

	if err != nil {
		kdexv1alpha1.SetConditions(
			&hostPackageReferences.Status.Conditions,
			kdexv1alpha1.ConditionStatuses{
				Degraded:    metav1.ConditionTrue,
				Progressing: metav1.ConditionFalse,
				Ready:       metav1.ConditionFalse,
			},
			kdexv1alpha1.ConditionReasonReconcileError,
			err.Error(),
		)

		return nil, err
	}

	log.Info("secret created", "secret", secret.Name, "operation", op)

	return secret, nil
}
