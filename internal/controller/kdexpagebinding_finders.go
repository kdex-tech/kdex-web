package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *KDexPageBindingReconciler) findPageBindingsForApp(
	ctx context.Context,
	app client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingsList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingsList, &client.ListOptions{
		Namespace: app.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for app", "name", app.GetName())
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}
	for _, pageBinding := range pageBindingsList.Items {
		for _, contentEntry := range pageBinding.Spec.ContentEntries {
			if contentEntry.AppRef == nil {
				continue
			}
			if contentEntry.AppRef.Name == app.GetName() {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      pageBinding.Name,
						Namespace: pageBinding.Namespace,
					},
				})
			}
		}
	}
	return requests
}

func (r *KDexPageBindingReconciler) findPageBindingsForHost(
	ctx context.Context,
	host client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingsList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingsList, &client.ListOptions{
		Namespace: host.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for host", "name", host.GetName())
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}
	for _, pageBinding := range pageBindingsList.Items {
		if pageBinding.Spec.HostRef.Name == host.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pageBinding.Name,
					Namespace: pageBinding.Namespace,
				},
			})
		}
	}
	return requests
}

func (r *KDexPageBindingReconciler) findPageBindingsForPageArchetype(
	ctx context.Context,
	pageArchetype client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingsList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingsList, &client.ListOptions{
		Namespace: pageArchetype.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for page archetype", "name", pageArchetype.GetName())
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}
	for _, pageBinding := range pageBindingsList.Items {
		if pageBinding.Spec.PageArchetypeRef.Name == pageArchetype.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pageBinding.Name,
					Namespace: pageBinding.Namespace,
				},
			})
		}
	}
	return requests
}

func (r *KDexPageBindingReconciler) findPageBindingsForPageBindings(
	ctx context.Context,
	parentPageBinding client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingsList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingsList, &client.ListOptions{
		Namespace: parentPageBinding.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for page binding", "name", parentPageBinding.GetName())
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}
	for _, pageBinding := range pageBindingsList.Items {
		if pageBinding.Spec.ParentPageRef == nil {
			continue
		}
		if pageBinding.Spec.ParentPageRef.Name == parentPageBinding.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pageBinding.Name,
					Namespace: pageBinding.Namespace,
				},
			})
		}
	}
	return requests
}

func (r *KDexPageBindingReconciler) findPageBindingsForPageFooter(
	ctx context.Context,
	pageFooter client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingsList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingsList, &client.ListOptions{
		Namespace: pageFooter.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for page footer", "footer", pageFooter.GetName())
		return []reconcile.Request{}
	}

	var pageArchetypesList kdexv1alpha1.KDexPageArchetypeList
	if err := r.List(ctx, &pageArchetypesList, &client.ListOptions{
		Namespace: pageFooter.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageArchetypes for page footer", "footer", pageFooter.GetName())
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}

	for _, pageBinding := range pageBindingsList.Items {
		if pageBinding.Spec.OverrideFooterRef == nil {
			continue
		}
		if pageBinding.Spec.OverrideFooterRef.Name == pageFooter.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pageBinding.Name,
					Namespace: pageBinding.Namespace,
				},
			})
		}
	}

	for _, pageArchetype := range pageArchetypesList.Items {
		if pageArchetype.Spec.DefaultFooterRef == nil {
			continue
		}
		if pageArchetype.Spec.DefaultFooterRef.Name == pageFooter.GetName() {
			for _, pageBinding := range pageBindingsList.Items {
				if pageBinding.Spec.PageArchetypeRef.Name == pageArchetype.GetName() {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      pageBinding.Name,
							Namespace: pageBinding.Namespace,
						},
					})
				}
			}
		}
	}

	return requests
}

func (r *KDexPageBindingReconciler) findPageBindingsForPageHeader(
	ctx context.Context,
	pageHeader client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingsList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingsList, &client.ListOptions{
		Namespace: pageHeader.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for page header", "name", pageHeader.GetName())
		return []reconcile.Request{}
	}

	var pageArchetypesList kdexv1alpha1.KDexPageArchetypeList
	if err := r.List(ctx, &pageArchetypesList, &client.ListOptions{
		Namespace: pageHeader.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageArchetypes for page header", "name", pageHeader.GetName())
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}

	for _, pageBinding := range pageBindingsList.Items {
		if pageBinding.Spec.OverrideHeaderRef == nil {
			continue
		}
		if pageBinding.Spec.OverrideHeaderRef.Name == pageHeader.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pageBinding.Name,
					Namespace: pageBinding.Namespace,
				},
			})
		}
	}

	for _, pageArchetype := range pageArchetypesList.Items {
		if pageArchetype.Spec.DefaultHeaderRef == nil {
			continue
		}
		if pageArchetype.Spec.DefaultHeaderRef.Name == pageHeader.GetName() {
			for _, pageBinding := range pageBindingsList.Items {
				if pageBinding.Spec.PageArchetypeRef.Name == pageArchetype.GetName() {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      pageBinding.Name,
							Namespace: pageBinding.Namespace,
						},
					})
				}
			}
		}
	}

	return requests
}

func (r *KDexPageBindingReconciler) findPageBindingsForPageNavigations(
	ctx context.Context,
	pageNavigation client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingsList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingsList, &client.ListOptions{
		Namespace: pageNavigation.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for page navigation", "navigation", pageNavigation.GetName())
		return []reconcile.Request{}
	}

	var pageArchetypesList kdexv1alpha1.KDexPageArchetypeList
	if err := r.List(ctx, &pageArchetypesList, &client.ListOptions{
		Namespace: pageNavigation.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageArchetypes for page navigation", "navigation", pageNavigation.GetName())
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}

	for _, pageBinding := range pageBindingsList.Items {
		if pageBinding.Spec.OverrideMainNavigationRef == nil {
			continue
		}
		if pageBinding.Spec.OverrideMainNavigationRef.Name == pageNavigation.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pageBinding.Name,
					Namespace: pageBinding.Namespace,
				},
			})
		}
	}

	for _, pageArchetype := range pageArchetypesList.Items {
		if pageArchetype.Spec.DefaultMainNavigationRef != nil {
			if pageArchetype.Spec.DefaultMainNavigationRef.Name == pageNavigation.GetName() {
				for _, pageBinding := range pageBindingsList.Items {
					if pageBinding.Spec.PageArchetypeRef.Name == pageArchetype.GetName() {
						requests = append(requests, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      pageBinding.Name,
								Namespace: pageBinding.Namespace,
							},
						})
					}
				}
			}
		}
		if pageArchetype.Spec.ExtraNavigations != nil {
			for _, navigationRef := range pageArchetype.Spec.ExtraNavigations {
				if navigationRef.Name == pageNavigation.GetName() {
					for _, pageBinding := range pageBindingsList.Items {
						if pageBinding.Spec.PageArchetypeRef.Name == pageArchetype.GetName() {
							requests = append(requests, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name:      pageBinding.Name,
									Namespace: pageBinding.Namespace,
								},
							})
						}
					}
				}
			}
		}
	}

	return requests
}

func (r *KDexPageBindingReconciler) findPageBindingsForScriptLibrary(
	ctx context.Context,
	scriptLibrary client.Object,
) []reconcile.Request {
	log := logf.FromContext(ctx)

	var pageBindingList kdexv1alpha1.KDexPageBindingList
	if err := r.List(ctx, &pageBindingList, &client.ListOptions{
		Namespace: scriptLibrary.GetNamespace(),
	}); err != nil {
		log.Error(err, "unable to list KDexPageBindings for scriptLibrary", "name", scriptLibrary.GetName())
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(pageBindingList.Items))
	for _, pageBinding := range pageBindingList.Items {
		if pageBinding.Spec.ScriptLibraryRef == nil {
			continue
		}
		if pageBinding.Spec.ScriptLibraryRef.Name == scriptLibrary.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pageBinding.Name,
					Namespace: pageBinding.Namespace,
				},
			})
		}
	}
	return requests
}
