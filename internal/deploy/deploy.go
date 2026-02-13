package deploy

import (
	"context"
	"crypto/sha256"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Deployer struct {
	Client         client.Client
	FaaSAdaptor    kdexv1alpha1.KDexFaaSAdaptorSpec
	Host           kdexv1alpha1.KDexInternalHost
	Scheme         *runtime.Scheme
	ServiceAccount string
}

// Runtime defines the interface for interacting with a FaaS provider.
type Runtime interface {
	// Deploy returns a Job that, when executed, will deploy or update the function.
	// The Job is expected to update the KDexFunction status upon completion.
	Deploy(ctx context.Context, function *kdexv1alpha1.KDexFunction) (*batchv1.Job, error)

	// Observe returns a workload that, when executed, calls the provider API to check status.
	// For external providers, this is likely a CronJob.
	// For K8s-native providers (like Knative), this might return nil (if handled by standard Watch),
	// or a no-op Job for consistency.
	Observe(ctx context.Context, function *kdexv1alpha1.KDexFunction) (client.Object, error)
}

// The FaaS adaptor is responsible for deploying the function.
// Since there are virtually unlimited number of ways to deploy a function,
// we use a job as a bridge between the Nexus controller and the FaaS adaptor.
// The workload provided by the FaaS adaptor knows how to deploy the function.
// Whether the functions are deployed on KNative, AWS Lambda, Google Cloud Functions,
// Azure Functions, or something else is irrelevant to the Nexus controller.
// The job is responsible for deploying the function and reporting the status
// of the deployment back to the Nexus controller. The job must return success or
// failure along with reasons, and upon success, at least the URL of the function so that the
// Focal Controller can mount it into the host's service mesh and dispatch requests to it.
func (d *Deployer) Deploy(ctx context.Context, function *kdexv1alpha1.KDexFunction) (*batchv1.Job, error) {
	// Create Job identity hash based on the image and the adaptor version
	image := ""
	if function.Status.Executable != nil {
		image = function.Status.Executable.Image
	}
	adaptorGen := function.Status.Attributes["faasAdaptor.generation"]

	h := sha256.New()
	h.Write([]byte(image))
	h.Write([]byte(adaptorGen))
	idHash := fmt.Sprintf("%x", h.Sum(nil))[:8]

	jobName := fmt.Sprintf("%s-deployer-%d-%s", function.Name, function.Generation, idHash)

	job := &batchv1.Job{}
	err := d.Client.Get(ctx, client.ObjectKey{Namespace: function.Namespace, Name: jobName}, job)
	if err == nil {
		return job, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	issuer := "http://" + d.Host.Spec.Routing.Domains[0]
	if d.Host.Spec.Routing.TLS != nil {
		issuer = "https://" + d.Host.Spec.Routing.Domains[0]
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
			Name:  "FUNCTION_NAMESPACE",
			Value: function.Namespace,
		},
		{
			Name:  "FUNCTION_BASEPATH",
			Value: function.Spec.API.BasePath,
		},
		{
			Name:  "JWKS_URL",
			Value: issuer + "/.well-known/jwks.json",
		},
		{
			Name:  "ISSUER",
			Value: issuer,
		},
		// {
		// 	Name:  "CLIENT_ID",
		// 	Value: d.Host.Spec.Auth.OIDCProvider.ClientID,
		// },
		{
			Name:  "AUDIENCE",
			Value: issuer,
		},
	}

	env = append(env, d.FaaSAdaptor.Deployer.Env...)

	if function.Status.Executable != nil {
		env = append(env, corev1.EnvVar{
			Name:  "FUNCTION_IMAGE",
			Value: function.Status.Executable.Image,
		})
	}

	forwardedEnvVars := ""
	sep := ""
	for _, e := range env {
		forwardedEnvVars += fmt.Sprintf("%s%s", sep, e.Name)
		sep = ","
	}

	env = append(env, corev1.EnvVar{
		Name:  "FORWARDED_ENV_VARS",
		Value: forwardedEnvVars,
	})

	job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: function.Namespace,
			Labels: map[string]string{
				"app":                 "deployer",
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
					Containers: []corev1.Container{
						{
							Args:    d.FaaSAdaptor.Deployer.Args,
							Command: d.FaaSAdaptor.Deployer.Command,
							Env:     env,
							Image:   d.FaaSAdaptor.Deployer.Image,

							// TODO: implement the KNative deployer image
							// TODO: implement the AWS Lambda deployer image
							// TODO: implement the Google Cloud Functions deployer image
							// TODO: implement the Azure Functions deployer image

							Name: "deployer",
						},
					},
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: d.ServiceAccount,
				},
			},
		},
	}

	// Add owner reference
	err = ctrl.SetControllerReference(function, job, d.Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment job: %w", err)
	}

	// Create the job
	err = d.Client.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment job: %w", err)
	}

	return job, nil
}

func (d *Deployer) Observe(ctx context.Context, function *kdexv1alpha1.KDexFunction) (client.Object, error) {
	if d.FaaSAdaptor.Observer == nil {
		return nil, nil // No observer configured
	}

	// Create CronJob name
	cronJobName := fmt.Sprintf("%s-observer", function.Name)

	cronJob := &batchv1.CronJob{}
	err := d.Client.Get(ctx, client.ObjectKey{Namespace: function.Namespace, Name: cronJobName}, cronJob)
	if err == nil {
		// Update schedule if changed? For now, just return existing.
		return cronJob, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	// Reuse deployment environment variables where appropriate
	env := []corev1.EnvVar{
		{
			Name:  "FUNCTION_HOST",
			Value: function.Spec.HostRef.Name,
		},
		{
			Name:  "FUNCTION_NAME",
			Value: function.Name,
		},
		{
			Name:  "FUNCTION_NAMESPACE",
			Value: function.Namespace,
		},
	}

	env = append(env, d.FaaSAdaptor.Observer.Env...)

	cronJob = &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: function.Namespace,
			Labels: map[string]string{
				"app":      "observer",
				"function": function.Name,
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          d.FaaSAdaptor.Observer.Schedule,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							AutomountServiceAccountToken: utils.Ptr(true),
							Containers: []corev1.Container{
								{
									Args:    d.FaaSAdaptor.Observer.Args,
									Command: d.FaaSAdaptor.Observer.Command,
									Env:     env,
									Image:   d.FaaSAdaptor.Observer.Image,
									Name:    "observer",
								},
							},
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							ServiceAccountName: d.FaaSAdaptor.Observer.ServiceAccountName,
						},
					},
				},
			},
		},
	}

	// Default service account if not set in observer spec
	if cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName == "" {
		cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName = d.ServiceAccount
	}

	// Add owner reference
	err = ctrl.SetControllerReference(function, cronJob, d.Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create observation cronjob: %w", err)
	}

	// Create the cronjob
	err = d.Client.Create(ctx, cronJob)
	if err != nil {
		return nil, fmt.Errorf("failed to create observation cronjob: %w", err)
	}

	return cronJob, nil
}
