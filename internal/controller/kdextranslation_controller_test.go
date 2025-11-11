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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("KDexTranslation Controller", func() {
	Context("When reconciling a resource", func() {
		const namespace = "default"
		const resourceName = "test-resource"

		ctx := context.Background()

		AfterEach(func() {
			By("Cleanup all the test resource instances")
			Expect(k8sClient.DeleteAllOf(ctx, &kdexv1alpha1.KDexHost{}, client.InNamespace(namespace))).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &kdexv1alpha1.KDexTranslation{}, client.InNamespace(namespace))).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			resource := &kdexv1alpha1.KDexTranslation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexTranslationSpec{
					HostRef: v1.LocalObjectReference{
						Name: focalHost,
					},
					Translations: []kdexv1alpha1.Translation{
						{
							Lang: "en",
							KeysAndValues: map[string]string{
								"key-1": "KEY_1_ENGLISH",
								"key-2": "KEY_2_ENGLISH",
							},
						},
						{
							Lang: "fr",
							KeysAndValues: map[string]string{
								"key-1": "KEY_1_FRENCH",
								"key-2": "KEY_2_FRENCH",
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexTranslation{}, false)

			hostResource := &kdexv1alpha1.KDexHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      focalHost,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexHostSpec{
					ModulePolicy: kdexv1alpha1.LooseModulePolicy,
					Organization: "KDex Tech Inc.",
					Routing: kdexv1alpha1.Routing{
						Domains:  []string{"foo.bar"},
						Strategy: kdexv1alpha1.IngressRoutingStrategy,
					},
				},
			}

			Expect(k8sClient.Create(ctx, hostResource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexTranslation{}, true)
		})
	})
})
