package controller

import (
	"os"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Defaults(configFile string) ResourceDefaults {
	FSGroup := int64(65532)
	ReadOnlyRootFilesystem := true
	Replicas := int32(1)
	RunAsNonRoot := true
	RunAsUser := int64(65532)

	defaults := ResourceDefaults{
		Container: ContainerDefaults{
			Port: 80,
			Resources: v1.ResourceRequirements{
				Limits: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1000m"),
					v1.ResourceMemory: resource.MustParse("1000Mi"),
				},
				Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("100m"),
					v1.ResourceMemory: resource.MustParse("100Mi"),
				},
			},
			SecurityContext: &v1.SecurityContext{
				Capabilities: &v1.Capabilities{
					Add: []v1.Capability{
						"NET_BIND_SERVICE",
					},
					Drop: []v1.Capability{
						"ALL",
					},
				},
				ReadOnlyRootFilesystem: &ReadOnlyRootFilesystem,
			},
		},
		Deployment: DeploymentDefaults{
			Image:      "kdex-tech/kdex-themeserver",
			PullPolicy: v1.PullIfNotPresent,
			Replicas:   &Replicas,
			SecurityContext: &v1.PodSecurityContext{
				FSGroup:      &FSGroup,
				RunAsNonRoot: &RunAsNonRoot,
				RunAsUser:    &RunAsUser,
			},
		},
		Theme: ThemeDefaults{
			PullPolicy: v1.PullIfNotPresent,
		},
	}

	in, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults
		}
		panic(err)
	}

	err = yaml.Unmarshal(in, &defaults)
	if err != nil {
		panic(err)
	}

	return defaults
}

func ControllerNamespace() string {
	in, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return os.Getenv("POD_NAMESPACE")
	}

	return string(in)
}
