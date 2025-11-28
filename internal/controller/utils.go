package controller

import (
	"context"
	"os"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/jsonpath"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var LikeNamedHandler = handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: o.GetName()}}}
})

func MakeHandlerByReferencePath(
	c client.Client,
	scheme *runtime.Scheme,
	watcherType client.Object,
	list client.ObjectList,
	referencePath string,
) handler.EventHandler {
	jpRef := jsonpath.New("ref-path")
	if err := jpRef.Parse(referencePath); err != nil {
		panic(err)
	}

	watcherKind, err := getKind(watcherType, scheme)
	if err != nil {
		panic(err)
	}

	log := logf.Log.WithName(
		"WATCH",
	).WithValues(
		"watcherKind", watcherKind,
		"referencePath", referencePath,
	)

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
		for _, item := range items {
			i := item.(client.Object)

			jsonPathReference, err := jpRef.FindResults(i)
			if err != nil {
				panic(err)
			}
			if len(jsonPathReference) == 0 || len(jsonPathReference[0]) == 0 {
				continue
			}

			ref := reflect.ValueOf(jsonPathReference[0][0].Interface())
			isNil := false
			switch ref.Kind() {
			case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
				isNil = ref.IsNil()
			}
			if ref.IsZero() || isNil {
				continue
			}

			theReferenceStruct := ref.Interface()

			switch v := theReferenceStruct.(type) {
			case corev1.LocalObjectReference:
				if v.Name == o.GetName() {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      i.GetName(),
							Namespace: i.GetNamespace(),
						},
					})
				}
			case *corev1.LocalObjectReference:
				if v.Name == o.GetName() {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      i.GetName(),
							Namespace: i.GetNamespace(),
						},
					})
				}
			case kdexv1alpha1.KDexObjectReference:
				namespace := i.GetNamespace()
				if v.Namespace != "" {
					namespace = v.Namespace
				}
				if v.Kind == objKind && v.Name == o.GetName() && i.GetNamespace() == namespace {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      i.GetName(),
							Namespace: i.GetNamespace(),
						},
					})
				}
			case *kdexv1alpha1.KDexObjectReference:
				namespace := i.GetNamespace()
				if v.Namespace != "" {
					namespace = v.Namespace
				}
				if v.Kind == objKind && v.Name == o.GetName() && i.GetNamespace() == namespace {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      i.GetName(),
							Namespace: i.GetNamespace(),
						},
					})
				}
			}
		}
		return requests
	})
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
