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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	primaryTemplate = `<!DOCTYPE html>
<html lang="{{ .Language }}">
	<head>
	{{ .Meta }}
	{{ .Title }}
	{{ .Theme }}
	{{ .HeadScript }}
	</head>
	<body>
	<header>
		{{ .Header }}
	</header>
	<nav>
		{{ .Navigation["main"] }}
	</nav>
	<main>
		{{ .Content["main"] }}
	</main>
	<footer>
		{{ .Footer }}
	</footer>
	{{ .FootScript }}
	</body>
</html>`
)

var _ = Describe("KDexRenderPage Controller", func() {
	Context("When reconciling a resource", func() {
		const namespace = "default"
		const resourceName = "test-resource"

		ctx := context.Background()

		AfterEach(func() {
			By("Cleanup all the test resource instances")
			Expect(k8sClient.DeleteAllOf(ctx, &kdexv1alpha1.KDexHost{}, client.InNamespace(namespace))).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &kdexv1alpha1.KDexRenderPage{}, client.InNamespace(namespace))).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			resource := &kdexv1alpha1.KDexRenderPage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexRenderPageSpec{
					HostRef: corev1.LocalObjectReference{
						Name: "test-host",
					},
					PageComponents: kdexv1alpha1.PageComponents{
						Contents: map[string]string{
							"main": "MAIN",
						},
						Footer: "FOOTER",
						Header: "HEADER",
						Navigations: map[string]string{
							"main": "NAV",
						},
						PrimaryTemplate: primaryTemplate,
						Title:           "TITLE",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexRenderPage{}, false)

			hostResource := &kdexv1alpha1.KDexHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-host",
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexHostSpec{
					AppPolicy: kdexv1alpha1.NonStrictAppPolicy,
					Domains: []string{
						"foo.bar.dev",
					},
					Organization: "KDex Tech Inc.",
				},
			}

			Expect(k8sClient.Create(ctx, hostResource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexRenderPage{}, true)
		})
	})
})
