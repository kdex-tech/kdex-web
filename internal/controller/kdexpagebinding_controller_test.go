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
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
)

var _ = Describe("KDexPageBinding Controller", func() {
	Context("When reconciling a resource", func() {
		const namespace = "default"
		const resourceName = "test-resource"

		ctx := context.Background()

		AfterEach(func() {
			cleanupResources(namespace)
		})

		It("with empty content entries should not succeed", func() {
			resource := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/foo",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
		})

		It("with content entries should succeed", func() {
			resource := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "<h1>Hello, World!</h1>",
							Slot:    "main",
						},
					},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/foo",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		It("with missing references should not succeed", func() {
			resource := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "<h1>Hello, World!</h1>",
							Slot:    "main",
						},
					},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/foo",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, false)

			addOrUpdateHost(
				ctx, k8sClient,
				kdexv1alpha1.KDexHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-host",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexHostSpec{
						BrandName:    "KDex Tech",
						ModulePolicy: kdexv1alpha1.LooseModulePolicy,
						Organization: "KDex Tech Inc.",
						Routing: kdexv1alpha1.Routing{
							Domains: []string{
								"example.com",
							},
							Strategy: kdexv1alpha1.IngressRoutingStrategy,
						},
					},
				},
			)

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, false)

			addOrUpdatePageArchetype(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageArchetype{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-page-archetype",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageArchetypeSpec{
						Content: "<h1>Hello, World!</h1>",
					},
				},
			)

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, true)
		})

		It("with override references", func() {
			resource := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "<h1>Hello, World!</h1>",
							Slot:    "main",
						},
					},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					OverrideFooterRef: &corev1.LocalObjectReference{
						Name: "non-existent-footer",
					},
					OverrideHeaderRef: &corev1.LocalObjectReference{
						Name: "non-existent-header",
					},
					OverrideMainNavigationRef: &corev1.LocalObjectReference{
						Name: "non-existent-navigation",
					},
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/foo",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, false)

			addOrUpdateHost(
				ctx, k8sClient,
				kdexv1alpha1.KDexHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-host",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexHostSpec{
						BrandName:    "KDex Tech",
						ModulePolicy: kdexv1alpha1.LooseModulePolicy,
						Organization: "KDex Tech Inc.",
						Routing: kdexv1alpha1.Routing{
							Domains: []string{
								"example.com",
							},
							Strategy: kdexv1alpha1.IngressRoutingStrategy,
						},
					},
				},
			)

			addOrUpdatePageArchetype(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageArchetype{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-page-archetype",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageArchetypeSpec{
						Content: "<h1>Hello, World!</h1>",
					},
				},
			)

			addOrUpdatePageFooter(ctx, k8sClient,
				kdexv1alpha1.KDexPageFooter{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-footer",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageFooterSpec{
						Content: "<h1>Hello, from down under!</h1>",
					},
				},
			)

			addOrUpdatePageHeader(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageHeader{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-header",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageHeaderSpec{
						Content: "<h1>Hello, from up north!</h1>",
					},
				},
			)

			addOrUpdatePageNavigation(ctx, k8sClient,
				kdexv1alpha1.KDexPageNavigation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-navigation",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageNavigationSpec{
						Content: "<h1>Hello, from up north!</h1>",
					},
				},
			)

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, true)
		})

		It("with parent page reference", func() {
			resource := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "<h1>Hello, World!</h1>",
							Slot:    "main",
						},
					},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					ParentPageRef: &corev1.LocalObjectReference{
						Name: "non-existent-page-binding",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/foo",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			addOrUpdateHost(
				ctx, k8sClient,
				kdexv1alpha1.KDexHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-host",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexHostSpec{
						BrandName:    "KDex Tech",
						ModulePolicy: kdexv1alpha1.LooseModulePolicy,
						Organization: "KDex Tech Inc.",
						Routing: kdexv1alpha1.Routing{
							Domains: []string{
								"example.com",
							},
							Strategy: kdexv1alpha1.IngressRoutingStrategy,
						},
					},
				},
			)

			addOrUpdatePageArchetype(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageArchetype{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-page-archetype",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageArchetypeSpec{
						Content: "<h1>Hello, World!</h1>",
					},
				},
			)

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, false)

			referencedPage := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent-page-binding",
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "<h1>Hello, World!</h1>",
							Slot:    "main",
						},
					},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/parent",
					},
				},
			}

			Expect(k8sClient.Create(ctx, referencedPage)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, true)
		})

		It("updates when a dependency is updated", func() {
			resource := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "<h1>Hello, World!</h1>",
							Slot:    "main",
						},
					},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					OverrideHeaderRef: &corev1.LocalObjectReference{
						Name: "non-existent-header",
					},
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/foo",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, false)

			addOrUpdateHost(
				ctx, k8sClient,
				kdexv1alpha1.KDexHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-host",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexHostSpec{
						BrandName:    "KDex Tech",
						ModulePolicy: kdexv1alpha1.LooseModulePolicy,
						Organization: "KDex Tech Inc.",
						Routing: kdexv1alpha1.Routing{
							Domains: []string{
								"example.com",
							},
							Strategy: kdexv1alpha1.IngressRoutingStrategy,
						},
					},
				},
			)

			addOrUpdatePageArchetype(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageArchetype{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-page-archetype",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageArchetypeSpec{
						Content: "<h1>Hello, World!</h1>",
					},
				},
			)

			addOrUpdatePageHeader(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageHeader{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-header",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageHeaderSpec{
						Content: "BEFORE",
					},
				},
			)

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, true)

			var checkPageBinding kdexv1alpha1.KDexPageBinding
			pageBindingName := types.NamespacedName{
				Name:      resourceName,
				Namespace: namespace,
			}
			err := k8sClient.Get(ctx, pageBindingName, &checkPageBinding)
			Expect(err).NotTo(HaveOccurred())

			resourceVersion := checkPageBinding.ResourceVersion

			addOrUpdatePageHeader(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageHeader{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-header",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageHeaderSpec{
						Content: "AFTER",
					},
				},
			)

			check := func(g Gomega) {
				var checkPageBinding kdexv1alpha1.KDexPageBinding
				pageBindingName := types.NamespacedName{
					Name:      resourceName,
					Namespace: namespace,
				}
				err := k8sClient.Get(ctx, pageBindingName, &checkPageBinding)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(checkPageBinding.ResourceVersion).NotTo(Equal(resourceVersion))
			}

			Eventually(check).Should(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, true)
		})

		It("updates when an indirect dependency is updated", func() {
			resource := &kdexv1alpha1.KDexPageBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: kdexv1alpha1.KDexPageBindingSpec{
					ContentEntries: []kdexv1alpha1.ContentEntry{
						{
							RawHTML: "<h1>Hello, World!</h1>",
							Slot:    "main",
						},
					},
					HostRef: corev1.LocalObjectReference{
						Name: "non-existent-host",
					},
					Label: "foo",
					PageArchetypeRef: corev1.LocalObjectReference{
						Name: "non-existent-page-archetype",
					},
					Paths: kdexv1alpha1.Paths{
						BasePath: "/foo",
					},
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, false)

			addOrUpdateHost(
				ctx, k8sClient,
				kdexv1alpha1.KDexHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-host",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexHostSpec{
						BrandName:    "KDex Tech",
						ModulePolicy: kdexv1alpha1.LooseModulePolicy,
						Organization: "KDex Tech Inc.",
						Routing: kdexv1alpha1.Routing{
							Domains: []string{
								"example.com",
							},
							Strategy: kdexv1alpha1.IngressRoutingStrategy,
						},
					},
				},
			)

			addOrUpdatePageArchetype(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageArchetype{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-page-archetype",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageArchetypeSpec{
						Content: "<h1>Hello, World!</h1>",
						DefaultHeaderRef: &corev1.LocalObjectReference{
							Name: "non-existent-header",
						},
					},
				},
			)

			addOrUpdatePageHeader(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageHeader{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-header",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageHeaderSpec{
						Content: "BEFORE",
					},
				},
			)

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, true)

			var checkPageBinding kdexv1alpha1.KDexPageBinding
			pageBindingName := types.NamespacedName{
				Name:      resourceName,
				Namespace: namespace,
			}
			err := k8sClient.Get(ctx, pageBindingName, &checkPageBinding)
			Expect(err).NotTo(HaveOccurred())

			resourceVersion := checkPageBinding.ResourceVersion

			addOrUpdatePageHeader(
				ctx, k8sClient,
				kdexv1alpha1.KDexPageHeader{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent-header",
						Namespace: namespace,
					},
					Spec: kdexv1alpha1.KDexPageHeaderSpec{
						Content: "AFTER",
					},
				},
			)

			check := func(g Gomega) {
				var checkPageBinding kdexv1alpha1.KDexPageBinding
				pageBindingName := types.NamespacedName{
					Name:      resourceName,
					Namespace: namespace,
				}
				err := k8sClient.Get(ctx, pageBindingName, &checkPageBinding)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(checkPageBinding.ResourceVersion).NotTo(Equal(resourceVersion))
			}

			Eventually(check).Should(Succeed())

			assertResourceReady(
				ctx, k8sClient, resourceName, namespace,
				&kdexv1alpha1.KDexPageBinding{}, true)
		})
	})
})
