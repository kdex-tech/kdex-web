package controller

import corev1 "k8s.io/api/core/v1"

type ContainerDefaults struct {
	Port            int32                       `json:"port" yaml:"port"`
	Resources       corev1.ResourceRequirements `json:"resources" yaml:"resources"`
	SecurityContext *corev1.SecurityContext     `json:"securityContext" yaml:"securityContext"`
}

type DeploymentDefaults struct {
	Image           string                     `json:"image" yaml:"image"`
	PullPolicy      corev1.PullPolicy          `json:"pullPolicy" yaml:"pullPolicy"`
	Replicas        *int32                     `json:"replicas" yaml:"replicas"`
	SecurityContext *corev1.PodSecurityContext `json:"securityContext" yaml:"securityContext"`
}

type ResourceDefaults struct {
	Container  ContainerDefaults  `json:"container" yaml:"container"`
	Deployment DeploymentDefaults `json:"deployment" yaml:"deployment"`
	Theme      ThemeDefaults      `json:"theme" yaml:"theme"`
}

type ThemeDefaults struct {
	PullPolicy corev1.PullPolicy `json:"pullPolicy" yaml:"pullPolicy"`
}
