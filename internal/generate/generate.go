package generate

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/oasdiff/yaml"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kdex-tech/kdex-host/internal"
	"github.com/kdex-tech/kdex-host/internal/utils"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Generator struct {
	client.Client
	Config         kdexv1alpha1.Generator
	Scheme         *runtime.Scheme
	ServiceAccount string
}

func (g *Generator) GetOrCreateGenerateJob(ctx context.Context, function *kdexv1alpha1.KDexFunction) (*batchv1.Job, error) {
	// Create Job name
	jobName := fmt.Sprintf("%s-codegen-%d", function.Name, function.Generation)

	job := &batchv1.Job{}
	err := g.Get(ctx, client.ObjectKey{Namespace: function.Namespace, Name: jobName}, job)
	if err == nil {
		return job, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	url := function.Status.Attributes["openapi.schema.url.internal"]
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	functionString, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	env := []corev1.EnvVar{
		{
			Name:  "FUNCTION_HOST",
			Value: function.Spec.HostRef.Name,
		},
		{
			Name:  "FUNCTION_GENERATION",
			Value: fmt.Sprintf("%d", function.Generation),
		},
		{
			Name:  "FUNCTION_NAME",
			Value: function.Name,
		},
		{
			Name:  "FUNCTION_ENTRYPOINT",
			Value: g.Config.Entrypoint,
		},
		{
			Name:  "FUNCTION_NAMESPACE",
			Value: function.Namespace,
		},
		{
			Name:  "FUNCTION_BASEPATH",
			Value: function.Spec.API.BasePath,
		},
		{
			Name:  "FUNCTION_SPEC",
			Value: marshall(function),
		},
		{
			Name:  "FUNCTION_API_SPEC",
			Value: string(functionString),
		},
		{
			Name:  "COMMITTER_EMAIL",
			Value: g.Config.Git.CommitterEmail,
		},
		{
			Name:  "COMMITTER_NAME",
			Value: g.Config.Git.CommitterName,
		},
		{
			Name:  "COMMIT_SUB_DIRECTORY",
			Value: g.Config.Git.FunctionSubDirectory,
		},
		{
			Name: "GIT_HOST",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key:                  "host",
					LocalObjectReference: g.Config.Git.RepoSecretRef,
				},
			},
		},
		{
			Name: "GIT_ORG",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key:                  "org",
					LocalObjectReference: g.Config.Git.RepoSecretRef,
				},
			},
		},
		{
			Name: "GIT_REPO",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key:                  "repo",
					LocalObjectReference: g.Config.Git.RepoSecretRef,
				},
			},
		},
		{
			Name: "GIT_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key:                  "token",
					LocalObjectReference: g.Config.Git.RepoSecretRef,
				},
			},
		},
		{
			Name: "GIT_USER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key:                  "user",
					LocalObjectReference: g.Config.Git.RepoSecretRef,
				},
			},
		},
		{
			Name:  "WORKDIR",
			Value: internal.WORKDIR,
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      internal.SHARED_VOLUME,
			MountPath: internal.WORKDIR,
		},
	}

	job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: function.Namespace,
			Labels: map[string]string{
				"app":                 "codegen",
				"function":            function.Name,
				"kdex.dev/generation": fmt.Sprintf("%d", function.Generation),
			},
			Annotations: map[string]string{
				"kdex.dev/generation": fmt.Sprintf("%d", function.Generation),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: utils.Ptr(int32(3)),
			Completions:  utils.Ptr(int32(1)),
			Parallelism:  utils.Ptr(int32(1)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kdex.dev/generation": fmt.Sprintf("%d", function.Generation),
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: utils.Ptr(true),
					InitContainers: []corev1.Container{
						{
							Name:         "git-checkout",
							Image:        g.Config.Git.Image,
							Command:      []string{"git_checkout"},
							Env:          env,
							VolumeMounts: volumeMounts,
						},
						{
							Name:         "generate-code",
							Image:        g.Config.Image,
							Command:      g.Config.Command,
							Args:         g.Config.Args,
							Env:          env,
							VolumeMounts: volumeMounts,
						},
						{
							Name:         "git-push",
							Image:        g.Config.Git.Image,
							Command:      []string{"git_push"},
							Env:          env,
							VolumeMounts: volumeMounts,
						},
					},
					Containers: []corev1.Container{
						{
							Name:         "results",
							Image:        g.Config.Git.Image,
							Command:      []string{"patch_source_status"},
							Env:          env,
							VolumeMounts: volumeMounts,
						},
					},
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: g.ServiceAccount,
					Volumes: []corev1.Volume{
						{
							Name: internal.SHARED_VOLUME,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Add owner reference
	err = ctrl.SetControllerReference(function, job, g.Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create code generation job: %w", err)
	}

	// Create the job
	err = g.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to create code generation job: %w", err)
	}

	return job, nil
}

func marshall(function *kdexv1alpha1.KDexFunction) string {
	copy := function.DeepCopy()

	if copy.Status.Generator != nil && copy.Spec.Origin.Generator == nil {
		copy.Spec.Origin.Generator = copy.Status.Generator
	}
	if copy.Status.Source != nil && copy.Spec.Origin.Source == nil {
		copy.Spec.Origin.Source = copy.Status.Source
	}
	if copy.Status.Executable != nil && copy.Spec.Origin.Executable == nil {
		copy.Spec.Origin.Executable = copy.Status.Executable
	}

	copy.Status = kdexv1alpha1.KDexFunctionStatus{}
	copy.ObjectMeta.ManagedFields = nil
	copy.ObjectMeta.ResourceVersion = "0"
	copy.ObjectMeta.UID = "0"
	copy.ObjectMeta.CreationTimestamp = metav1.Time{}
	copy.ObjectMeta.DeletionTimestamp = nil
	copy.ObjectMeta.DeletionGracePeriodSeconds = nil
	copy.ObjectMeta.OwnerReferences = nil
	copy.ObjectMeta.Finalizers = nil
	copy.ObjectMeta.GenerateName = ""
	copy.ObjectMeta.Generation = 0
	yamlBytes, _ := yaml.Marshal(&copy)
	return string(yamlBytes)
}
