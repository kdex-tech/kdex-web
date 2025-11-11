package controller

import (
	"context"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Pairs struct {
	resource client.Object
	list     client.ObjectList
}

// func addOrUpdate(
// 	ctx context.Context,
// 	k8sClient client.Client,
// 	object client.Object,
// 	list client.ObjectList,
// ) {
// 	Eventually(func(g Gomega) error {
// 		err := k8sClient.List(ctx, list, &client.ListOptions{
// 			Namespace:     object.GetNamespace(),
// 			FieldSelector: fields.OneTermEqualSelector("metadata.name", object.GetName()),
// 		})
// 		g.Expect(err).NotTo(HaveOccurred())

// 		items, err := meta.ExtractList(list)
// 		g.Expect(err).NotTo(HaveOccurred())
// 		if len(items) > 0 {
// 			existing := items[0].(client.Object)
// 			existing.Spec = object.Spec
// 			g.Eventually(k8sClient.Update(ctx, existing)).Should(Succeed())
// 		} else {
// 			g.Expect(k8sClient.Create(ctx, object)).To(Succeed())
// 		}
// 		return nil
// 	}).Should(Succeed())
// }

func addOrUpdateHost(
	ctx context.Context,
	k8sClient client.Client,
	host kdexv1alpha1.KDexHost,
) {
	Eventually(func(g Gomega) error {
		list := &kdexv1alpha1.KDexHostList{}
		err := k8sClient.List(ctx, list, &client.ListOptions{
			Namespace:     host.Namespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", host.Name),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(list.Items) > 0 {
			existing := list.Items[0]
			existing.Spec = host.Spec
			g.Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
		} else {
			g.Expect(k8sClient.Create(ctx, &host)).To(Succeed())
		}
		return nil
	}).Should(Succeed())
}

func addOrUpdatePageArchetype(
	ctx context.Context,
	k8sClient client.Client,
	pageArchetype kdexv1alpha1.KDexPageArchetype,
) {
	Eventually(func(g Gomega) error {
		list := &kdexv1alpha1.KDexPageArchetypeList{}
		err := k8sClient.List(ctx, list, &client.ListOptions{
			Namespace:     pageArchetype.Namespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", pageArchetype.Name),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(list.Items) > 0 {
			existing := list.Items[0]
			existing.Spec = pageArchetype.Spec
			g.Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
		} else {
			g.Expect(k8sClient.Create(ctx, &pageArchetype)).To(Succeed())
		}
		return nil
	}).Should(Succeed())
}

func addOrUpdatePageHeader(
	ctx context.Context,
	k8sClient client.Client,
	pageHeader kdexv1alpha1.KDexPageHeader,
) {
	Eventually(func(g Gomega) error {
		list := &kdexv1alpha1.KDexPageHeaderList{}
		err := k8sClient.List(ctx, list, &client.ListOptions{
			Namespace:     pageHeader.Namespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", pageHeader.Name),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(list.Items) > 0 {
			existing := list.Items[0]
			existing.Spec = pageHeader.Spec
			g.Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
		} else {
			g.Expect(k8sClient.Create(ctx, &pageHeader)).To(Succeed())
		}
		return nil
	}).Should(Succeed())
}

func addOrUpdatePageFooter(
	ctx context.Context,
	k8sClient client.Client,
	pageFooter kdexv1alpha1.KDexPageFooter,
) {
	Eventually(func(g Gomega) error {
		list := &kdexv1alpha1.KDexPageFooterList{}
		err := k8sClient.List(ctx, list, &client.ListOptions{
			Namespace:     pageFooter.Namespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", pageFooter.Name),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(list.Items) > 0 {
			existing := list.Items[0]
			existing.Spec = pageFooter.Spec
			g.Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
		} else {
			g.Expect(k8sClient.Create(ctx, &pageFooter)).To(Succeed())
		}
		return nil
	}).Should(Succeed())
}

func addOrUpdatePageNavigation(
	ctx context.Context,
	k8sClient client.Client,
	pageNavigation kdexv1alpha1.KDexPageNavigation,
) {
	Eventually(func(g Gomega) error {
		list := &kdexv1alpha1.KDexPageNavigationList{}
		err := k8sClient.List(ctx, list, &client.ListOptions{
			Namespace:     pageNavigation.Namespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", pageNavigation.Name),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(list.Items) > 0 {
			existing := list.Items[0]
			existing.Spec = pageNavigation.Spec
			g.Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
		} else {
			g.Expect(k8sClient.Create(ctx, &pageNavigation)).To(Succeed())
		}
		return nil
	}).Should(Succeed())
}

func addOrUpdateScriptLibrary(
	ctx context.Context,
	k8sClient client.Client,
	scriptLibrary kdexv1alpha1.KDexScriptLibrary,
) {
	Eventually(func(g Gomega) error {
		list := &kdexv1alpha1.KDexScriptLibraryList{}
		err := k8sClient.List(ctx, list, &client.ListOptions{
			Namespace:     scriptLibrary.Namespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", scriptLibrary.Name),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(list.Items) > 0 {
			existing := list.Items[0]
			existing.Spec = scriptLibrary.Spec
			g.Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
		} else {
			g.Expect(k8sClient.Create(ctx, &scriptLibrary)).To(Succeed())
		}
		return nil
	}).Should(Succeed())
}

func addOrUpdateTheme(
	ctx context.Context,
	k8sClient client.Client,
	theme kdexv1alpha1.KDexTheme,
) {
	Eventually(func(g Gomega) error {
		list := &kdexv1alpha1.KDexThemeList{}
		err := k8sClient.List(ctx, list, &client.ListOptions{
			Namespace:     theme.Namespace,
			FieldSelector: fields.OneTermEqualSelector("metadata.name", theme.Name),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(list.Items) > 0 {
			existing := list.Items[0]
			existing.Spec = theme.Spec
			g.Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
		} else {
			g.Expect(k8sClient.Create(ctx, &theme)).To(Succeed())
		}
		return nil
	}).Should(Succeed())
}

func assertResourceReady(ctx context.Context, k8sClient client.Client, name string, namespace string, checkResource client.Object, ready bool) {
	typeNamespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	check := func(g Gomega) {
		err := k8sClient.Get(ctx, typeNamespacedName, checkResource)
		g.Expect(err).NotTo(HaveOccurred())
		it := reflect.ValueOf(checkResource).Elem()
		statusField := it.FieldByName("Status")
		g.Expect(statusField.IsValid()).To(BeTrue())
		conditionsField := statusField.FieldByName("Conditions")
		g.Expect(conditionsField.IsValid()).To(BeTrue())
		conditions, ok := conditionsField.Interface().([]metav1.Condition)
		g.Expect(ok).To(BeTrue())
		if ready {
			g.Expect(
				meta.IsStatusConditionTrue(
					conditions, string(kdexv1alpha1.ConditionTypeReady),
				),
			).To(BeTrue())
		} else {
			g.Expect(
				meta.IsStatusConditionFalse(
					conditions, string(kdexv1alpha1.ConditionTypeReady),
				),
			).To(BeTrue())
		}
	}

	Eventually(check, "5s").Should(Succeed())
}

func cleanupResources(namespace string) {
	By("Cleanup all the test resource instances")

	for _, pair := range []Pairs{
		{&kdexv1alpha1.KDexApp{}, &kdexv1alpha1.KDexAppList{}},
		{&kdexv1alpha1.KDexHost{}, &kdexv1alpha1.KDexHostList{}},
		{&kdexv1alpha1.KDexHostController{}, &kdexv1alpha1.KDexHostControllerList{}},
		{&kdexv1alpha1.KDexPageArchetype{}, &kdexv1alpha1.KDexPageArchetypeList{}},
		{&kdexv1alpha1.KDexPageBinding{}, &kdexv1alpha1.KDexPageBindingList{}},
		{&kdexv1alpha1.KDexPageFooter{}, &kdexv1alpha1.KDexPageFooterList{}},
		{&kdexv1alpha1.KDexPageHeader{}, &kdexv1alpha1.KDexPageHeaderList{}},
		{&kdexv1alpha1.KDexPageNavigation{}, &kdexv1alpha1.KDexPageNavigationList{}},
		{&kdexv1alpha1.KDexScriptLibrary{}, &kdexv1alpha1.KDexScriptLibraryList{}},
		{&kdexv1alpha1.KDexTheme{}, &kdexv1alpha1.KDexThemeList{}},
		{&corev1.Secret{}, &corev1.SecretList{}},
	} {
		err := k8sClient.DeleteAllOf(ctx, pair.resource, client.InNamespace(namespace))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) error {
			list := pair.list
			err := k8sClient.List(ctx, list, client.InNamespace(namespace))
			g.Expect(err).NotTo(HaveOccurred())
			items, err := meta.ExtractList(list)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(items).To(HaveLen(0))
			return nil
		}).To(Succeed())
	}
}
