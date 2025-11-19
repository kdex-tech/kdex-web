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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const hostPackageReferencesFinalizerName = "kdex.dev/hostpackagereferences-finalizer"

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

	cm, err := r.createOrUpdateJobConfigMap(ctx, &hostPackageReferences)
	if err != nil {
		return ctrl.Result{}, err
	}

	npmrcSecret, err := r.createOrUpdateNpmrcSecret(ctx, &hostPackageReferences)
	if err != nil {
		return ctrl.Result{}, err
	}

	return r.createOrUpdateJob(ctx, &hostPackageReferences, cm, npmrcSecret)
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
	configMap *corev1.ConfigMap,
	npmrcSecret *corev1.Secret,
) (ctrl.Result, error) {
	job := &batchv1.Job{}
	jobName := types.NamespacedName{
		Name:      hostPackageReferences.Name,
		Namespace: hostPackageReferences.Namespace,
	}

	if err := r.Get(ctx, jobName, job); err != nil {
		if errors.IsNotFound(err) {
			job, err = r.buildJob(ctx, hostPackageReferences, configMap, npmrcSecret)
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

			if err := r.Create(ctx, job); err != nil {
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

			kdexv1alpha1.SetConditions(
				&hostPackageReferences.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionFalse,
					Progressing: metav1.ConditionTrue,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				"Job created",
			)

			return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
		}

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

	if job.Labels["kdex.dev/generation"] != fmt.Sprintf("%d", hostPackageReferences.Generation) {
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			kdexv1alpha1.SetConditions(
				&hostPackageReferences.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				"Deleting job failed",
			)

			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: r.RequeueDelay}, nil
	}

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

		imageRepo := r.Configuration.DefaultImageRegistry.Host
		imageURL := fmt.Sprintf("%s/%s@%s", imageRepo, hostPackageReferences.Name, imageDigest)
		hostPackageReferences.Status.Image = imageURL

		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			kdexv1alpha1.SetConditions(
				&hostPackageReferences.Status.Conditions,
				kdexv1alpha1.ConditionStatuses{
					Degraded:    metav1.ConditionTrue,
					Progressing: metav1.ConditionFalse,
					Ready:       metav1.ConditionFalse,
				},
				kdexv1alpha1.ConditionReasonReconcileError,
				"Deleting job failed",
			)

			return ctrl.Result{}, err
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

func (r *KDexHostPackageReferencesReconciler) buildJob(
	ctx context.Context,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
	configMap *corev1.ConfigMap,
	npmrcSecret *corev1.Secret,
) (*batchv1.Job, error) {
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
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostPackageReferences.Name,
			Namespace: hostPackageReferences.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name": kdexWeb,
				"kdex.dev/generation":    fmt.Sprintf("%d", hostPackageReferences.Generation),
				"kdex.dev/packages":      hostPackageReferences.Name,
			},
		},
		Spec: batchv1.JobSpec{
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
							Name:         "npm-install",
							Image:        "node:18-alpine",
							Command:      []string{"sh", "-c", "cp /scripts/package.json /workspace/package.json && cd /workspace && npm install"},
							VolumeMounts: npmInstallVolumeMounts,
						},
					},
					RestartPolicy: "Never",
					Volumes:       volumes,
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(hostPackageReferences, job, r.Scheme); err != nil {
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

	return job, nil
}

func (r *KDexHostPackageReferencesReconciler) createOrUpdateJobConfigMap(
	ctx context.Context,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-configmap", hostPackageReferences.Name),
			Namespace: hostPackageReferences.Namespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		configMap,
		func() error {
			packageJSON := `{"dependencies": {`
			for i, pkg := range hostPackageReferences.Spec.PackageReferences {
				if i > 0 {
					packageJSON += ","
				}
				packageJSON += fmt.Sprintf(`"%s": "%s"`, pkg.Name, pkg.Version)
			}
			packageJSON += `}}`

			dockerfile := `
FROM scratch
COPY node_modules /modules
`

			if configMap.Annotations == nil {
				configMap.Annotations = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Annotations {
				configMap.Annotations[key] = value
			}
			if configMap.Labels == nil {
				configMap.Labels = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Labels {
				configMap.Labels[key] = value
			}

			configMap.Labels["kdex.dev/packages"] = hostPackageReferences.Name

			configMap.Data = map[string]string{
				"package.json": packageJSON,
				"Dockerfile":   dockerfile,
			}

			return ctrl.SetControllerReference(hostPackageReferences, configMap, r.Scheme)
		},
	); err != nil {
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

	return configMap, nil
}

func (r *KDexHostPackageReferencesReconciler) createOrUpdateNpmrcSecret(
	ctx context.Context,
	hostPackageReferences *kdexv1alpha1.KDexHostPackageReferences,
) (*corev1.Secret, error) {
	if r.Configuration.DefaultNpmRegistry.Host == "" {
		return nil, nil
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-secret", hostPackageReferences.Name),
			Namespace: hostPackageReferences.Namespace,
		},
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

	if _, err := ctrl.CreateOrUpdate(
		ctx,
		r.Client,
		secret,
		func() error {
			npmrcContent := fmt.Sprintf("registry=%s\n", r.Configuration.DefaultNpmRegistry.GetAddress())
			if r.Configuration.DefaultNpmRegistry.AuthData.Token != "" {
				npmrcContent += fmt.Sprintf("//%s/:_authToken=%s\n", registryUrl.Host, r.Configuration.DefaultNpmRegistry.AuthData.Token)
			} else if r.Configuration.DefaultNpmRegistry.AuthData.Username != "" && r.Configuration.DefaultNpmRegistry.AuthData.Password != "" {
				authStr := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", r.Configuration.DefaultNpmRegistry.AuthData.Username, r.Configuration.DefaultNpmRegistry.AuthData.Password)))
				npmrcContent += fmt.Sprintf("//%s/:_auth=%s\n", registryUrl.Host, authStr)
			}

			if secret.Annotations == nil {
				secret.Annotations = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Annotations {
				secret.Annotations[key] = value
			}
			if secret.Labels == nil {
				secret.Labels = make(map[string]string)
			}
			for key, value := range hostPackageReferences.Labels {
				secret.Labels[key] = value
			}

			secret.Labels["kdex.dev/packages"] = hostPackageReferences.Name

			secret.StringData = map[string]string{
				".npmrc": npmrcContent,
			}

			return ctrl.SetControllerReference(hostPackageReferences, secret, r.Scheme)
		},
	); err != nil {
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

	return secret, nil
}
