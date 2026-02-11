package deploy

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Deployer struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	Configuration  configuration.NexusConfiguration
	ServiceAccount string
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
func (d *Deployer) GetOrCreateDeployJob(ctx context.Context, function *kdexv1alpha1.KDexFunction, faasAdaptorSpec *kdexv1alpha1.KDexFaaSAdaptorSpec) (*batchv1.Job, error) {
	// Create Job name
	jobName := fmt.Sprintf("%s-deployer-%d", function.Name, function.Generation)

	job := &batchv1.Job{}
	err := d.Client.Get(ctx, client.ObjectKey{Namespace: function.Namespace, Name: jobName}, job)
	if err == nil {
		return job, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	return nil, nil
}
