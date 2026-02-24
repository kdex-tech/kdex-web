package packref

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kdex-tech/kdex-host/internal"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PackRef struct {
	client.Client
	ImageRegistry     kdexv1alpha1.Registry
	ConfigMap         *corev1.ConfigMap
	Log               logr.Logger
	NPMSecretRef      *corev1.LocalObjectReference
	Scheme            *runtime.Scheme
	ServiceAccountRef corev1.LocalObjectReference
}

func (p *PackRef) GetOrCreatePackRefJob(ctx context.Context, ipr *kdexv1alpha1.KDexInternalPackageReferences) (*batchv1.Job, error) {
	jobName := fmt.Sprintf("%s-packages-%d", ipr.Name, ipr.Generation)

	job := &batchv1.Job{}
	err := p.Get(ctx, client.ObjectKey{Namespace: ipr.Namespace, Name: jobName}, job)
	if err == nil {
		return job, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	env := []corev1.EnvVar{
		{
			Name:  "WORKDIR",
			Value: internal.WORKDIR,
		},
	}

	imageTag := fmt.Sprintf("%d", ipr.Generation)
	imageURL := fmt.Sprintf("%s/%s:%s", p.ImageRegistry.Host, ipr.Name, imageTag)

	builderArgs := []string{
		"--dockerfile=/scripts/Dockerfile",
		"--context=dir://" + internal.WORKDIR,
		"--destination=" + imageURL,
		"--digest-file=/dev/termination-log",
	}

	if p.ImageRegistry.Insecure {
		builderArgs = append(builderArgs, "--insecure")
	}

	volumes := []corev1.Volume{
		{
			Name: internal.SHARED_VOLUME,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "build-scripts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: p.ConfigMap.Name,
					},
				},
			},
		},
	}

	if p.NPMSecretRef != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "npmrc",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: p.NPMSecretRef.Name,
				},
			},
		})
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      internal.SHARED_VOLUME,
			MountPath: internal.WORKDIR,
		},
		{
			Name:      "build-scripts",
			MountPath: "/scripts",
			ReadOnly:  true,
		},
	}

	if p.NPMSecretRef != nil {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "npmrc",
			MountPath: internal.WORKDIR + "/.npmrc",
			SubPath:   ".npmrc",
			ReadOnly:  true,
		})
	}

	job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ipr.Namespace,
			Labels: map[string]string{
				"app":                 "packages",
				"packages":            ipr.Name,
				"kdex.dev/generation": fmt.Sprintf("%d", ipr.Generation),
			},
			Annotations: map[string]string{
				"kdex.dev/generation": fmt.Sprintf("%d", ipr.Generation),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: new(int32(3)),
			Completions:  new(int32(1)),
			Parallelism:  new(int32(1)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kdex.dev/generation": fmt.Sprintf("%d", ipr.Generation),
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(true),
					Containers: []corev1.Container{
						{
							Name: "kaniko",

							Args:         builderArgs,
							Env:          env,
							Image:        "gcr.io/kaniko-project/executor:latest",
							VolumeMounts: volumeMounts,
						},
					},
					ImagePullSecrets: ipr.Spec.BuilderImagePullSecrets,
					InitContainers: []corev1.Container{
						{
							Name: "npm-build",

							Command: []string{
								"sh",
								"-c",
								`set -e

cp /scripts/package.json ${WORKDIR}/package.json

cd ${WORKDIR}

echo "======== package.json ========="
cat package.json
echo -e "\n==============================="

npm install

npx esbuild node_modules/**/*.js --allow-overwrite --outdir=node_modules --define:process.env.NODE_ENV=\"production\"
`,
							},
							Env:          env,
							Image:        ipr.Spec.BuilderImage,
							VolumeMounts: volumeMounts,
						},
						{
							Name: "importmap-generator",

							Command: []string{
								"sh",
								"-c",
								`set -e

cp /scripts/generate.js ${WORKDIR}/generate.js

cd ${WORKDIR}

node generate.js

cat importmap.json > /dev/termination-log
						`,
							},
							Env:          env,
							Image:        ipr.Spec.BuilderImage,
							VolumeMounts: volumeMounts,
						},
					},
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: p.ServiceAccountRef.Name,
					Volumes:            volumes,
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(ipr, job, p.Scheme); err != nil {
		return nil, fmt.Errorf("failed to create packages job: %w", err)
	}

	if err = p.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create packages job: %w", err)
	}

	return job, nil
}
