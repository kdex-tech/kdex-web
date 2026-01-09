package controller

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/jsonpath"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/crds/configuration"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func CollectBackend(conf configuration.NexusConfiguration, requiredBackends *[]kdexv1alpha1.KDexObjectReference, obj client.Object) {
	if obj == nil {
		return
	}
	var backend kdexv1alpha1.Backend
	switch v := obj.(type) {
	case *kdexv1alpha1.KDexClusterApp:
		backend = v.Spec.Backend
	case *kdexv1alpha1.KDexClusterScriptLibrary:
		backend = v.Spec.Backend
	case *kdexv1alpha1.KDexClusterTheme:
		backend = v.Spec.Backend
	case *kdexv1alpha1.KDexApp:
		backend = v.Spec.Backend
	case *kdexv1alpha1.KDexScriptLibrary:
		backend = v.Spec.Backend
	case *kdexv1alpha1.KDexTheme:
		backend = v.Spec.Backend
	}

	if backend.StaticImage != "" || backend.ServerImage != conf.BackendDefault.ServerImage {
		ref := kdexv1alpha1.KDexObjectReference{
			Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		}
		found := false
		for _, rb := range *requiredBackends {
			if rb.Name == ref.Name && rb.Kind == ref.Kind && rb.Namespace == ref.Namespace {
				found = true
				break
			}
		}
		if !found {
			*requiredBackends = append(*requiredBackends, ref)
		}
	}
}

func MakeHandlerByReferencePath(
	c client.Client,
	scheme *runtime.Scheme,
	watcherType client.Object,
	list client.ObjectList,
	referencePath ...string,
) handler.EventHandler {
	watcherKind, err := getKind(watcherType, scheme)
	if err != nil {
		panic(err)
	}

	log := logf.Log.WithName(
		strings.ToLower(watcherKind),
	).WithName(
		"watch",
	).WithValues(
		"referencePaths", referencePath,
	)

	jpRefs := make([]*jsonpath.JSONPath, len(referencePath))
	for i, refPath := range referencePath {
		jpRefs[i] = jsonpath.New("ref-path-" + strconv.Itoa(i))
		if err := jpRefs[i].Parse(refPath); err != nil {
			panic(err)
		}
	}

	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		objKind, err := getKind(o, scheme)
		if err != nil {
			log.Error(err, "failed to get object kind", "object", o)
			return []reconcile.Request{}
		}

		if err := c.List(ctx, list, &client.ListOptions{
			Namespace: o.GetNamespace(),
		}); err != nil {
			return []reconcile.Request{}
		}

		items, err := meta.ExtractList(list)
		if err != nil || len(items) == 0 {
			return []reconcile.Request{}
		}

		requests := []reconcile.Request{}
		for _, i := range items {
			item := i.(client.Object)

			log.V(2).Info("processing item", "object", item.GetName(), "namespace", item.GetNamespace())

			for j, jpRef := range jpRefs {
				curPath := referencePath[j]
				jsonPathReference, err := jpRef.FindResults(item)
				if err != nil {
					log.V(2).Info("skipping", "path", curPath, "err", err, "object", item.GetName(), "namespace", item.GetNamespace())
					continue
				}
				if len(jsonPathReference) == 0 || len(jsonPathReference[0]) == 0 {
					log.V(2).Info("skipping", "path", curPath, "object", item.GetName(), "namespace", item.GetNamespace())
					continue
				}

				log.V(2).Info("found", "path", curPath, "object", item.GetName(), "namespace", item.GetNamespace())

				for idx, node := range jsonPathReference {
					for _, curRef := range node {
						ref := reflect.ValueOf(curRef.Interface())

						log.V(2).Info("reference", "reference", ref, "object", item.GetName(), "node", idx)

						isNil := false
						switch ref.Kind() {
						case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
							isNil = ref.IsNil()
						}
						if ref.IsZero() || isNil {
							continue
						}

						theReferenceStruct := ref.Interface()

						log.V(2).Info("struct", "interface", theReferenceStruct, "object", item.GetName(), "node", idx)

						switch v := theReferenceStruct.(type) {
						case corev1.LocalObjectReference:
							if v.Name == o.GetName() {
								requests = append(requests, reconcile.Request{
									NamespacedName: types.NamespacedName{
										Name:      item.GetName(),
										Namespace: item.GetNamespace(),
									},
								})
							}
						case *corev1.LocalObjectReference:
							if v.Name == o.GetName() {
								requests = append(requests, reconcile.Request{
									NamespacedName: types.NamespacedName{
										Name:      item.GetName(),
										Namespace: item.GetNamespace(),
									},
								})
							}
						case kdexv1alpha1.KDexObjectReference:
							namespace := item.GetNamespace()
							if v.Namespace != "" {
								namespace = v.Namespace
							}
							if v.Kind == objKind && v.Name == o.GetName() && item.GetNamespace() == namespace {
								requests = append(requests, reconcile.Request{
									NamespacedName: types.NamespacedName{
										Name:      item.GetName(),
										Namespace: item.GetNamespace(),
									},
								})
							}
						case *kdexv1alpha1.KDexObjectReference:
							namespace := item.GetNamespace()
							if v.Namespace != "" {
								namespace = v.Namespace
							}
							if v.Kind == objKind && v.Name == o.GetName() && item.GetNamespace() == namespace {
								requests = append(requests, reconcile.Request{
									NamespacedName: types.NamespacedName{
										Name:      item.GetName(),
										Namespace: item.GetNamespace(),
									},
								})
							}
						}
					}
				}
			}
		}
		return requests
	})
}

func LogConstructor(name string, mgr ctrl.Manager) func(request *reconcile.Request) logr.Logger {
	return func(request *reconcile.Request) logr.Logger {
		l := mgr.GetControllerOptions().Logger.WithName(name)
		if request != nil {
			l = l.WithValues("namespace", request.Namespace, "name", request.Name)
		}
		return l
	}
}

func ControllerNamespace() string {
	in, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return os.Getenv("POD_NAMESPACE")
	}

	return string(in)
}

func getKind(obj client.Object, scheme *runtime.Scheme) (string, error) {
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return "", err
	}
	return gvk.Kind, nil
}
