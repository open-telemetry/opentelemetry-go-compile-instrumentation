// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8s_client_go

import (
	"context"
	"reflect"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otelc/instrumentation/k8s.io/client-go/semconv"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
)

type k8SOtelEventHandler struct {
	handler cache.ResourceEventHandler
	ctx     context.Context
}

func newK8SOtelEventHandler(handler cache.ResourceEventHandler, ctx context.Context) *k8SOtelEventHandler {
	return &k8SOtelEventHandler{handler, ctx}
}

func (h k8SOtelEventHandler) OnAdd(obj any, isInInitialList bool) {
	objInfo := getObjectInfo(obj)
	attrs := semconv.K8SObjectInfoTraceAttrs(objInfo)

	spanName := getSpanName(objInfo.Kind, "add")
	_, span := tracer.Start(h.ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	h.handler.OnAdd(obj, isInInitialList)
}

func (h k8SOtelEventHandler) OnUpdate(oldObj, newObj any) {
	objInfo := getObjectInfo(newObj)
	attrs := semconv.K8SObjectInfoTraceAttrs(objInfo)

	spanName := getSpanName(objInfo.Kind, "update")
	_, span := tracer.Start(h.ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	h.handler.OnUpdate(oldObj, newObj)
}

func (h k8SOtelEventHandler) OnDelete(obj any) {
	// Unwrap only for metadata extraction: a DeletedFinalStateUnknown tombstone
	// means client-go's cache lost track of the object (e.g. a missed watch
	// event during a resync), so the delete is inferred rather than confirmed.
	// The wrapped user handler needs the original, possibly-wrapped obj to make
	// that same determination itself; forwarding the unwrapped value would
	// silently change observable application behavior under instrumentation.
	metaObj := obj
	if o, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		metaObj = o.Obj
	}

	objInfo := getObjectInfo(metaObj)
	attrs := semconv.K8SObjectInfoTraceAttrs(objInfo)

	spanName := getSpanName(objInfo.Kind, "delete")
	_, span := tracer.Start(h.ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	h.handler.OnDelete(obj)
}

func getSpanName(kind, action string) string {
	if len(kind) > 0 {
		return "k8s.informer." + strings.ToLower(kind) + "." + action
	}
	return "k8s.informer.object." + action
}

// gvkCache caches GVK lookups for typed objects, keyed by reflect.Type.
// Unstructured objects never reach this cache; see lookupGVK.
var gvkCache sync.Map // reflect.Type -> gvkLookup

type gvkLookup struct {
	kind       string
	apiVersion string
	ok         bool
}

// objectKindsFunc is scheme.Scheme.ObjectKinds, indirected so tests can
// substitute a counting stub to verify the cache actually avoids repeat
// lookups rather than just asserting the returned values are correct.
var objectKindsFunc = scheme.Scheme.ObjectKinds

func lookupGVK(runtimeObj runtime.Object) gvkLookup {
	// Unstructured objects carry their own GVK; every CRD shares the single
	// Go type unstructured.Unstructured, so they must never touch the
	// reflect.Type cache below, which would collide unrelated CRDs together.
	if _, isUnstructured := runtimeObj.(runtime.Unstructured); isUnstructured {
		gvk := runtimeObj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			return gvkLookup{}
		}
		return gvkLookup{kind: gvk.Kind, apiVersion: gvk.GroupVersion().String(), ok: true}
	}

	// Typed objects (e.g. *corev1.Pod) are safe to cache indefinitely by
	// reflect.Type: the scheme registry is fixed once built at startup, so
	// one Go type only ever maps to one GVK.
	t := reflect.TypeOf(runtimeObj)
	if cached, hit := gvkCache.Load(t); hit {
		return cached.(gvkLookup) //nolint:forcetypeassert // only this function ever stores into gvkCache
	}

	gvks, _, err := objectKindsFunc(runtimeObj)
	result := gvkLookup{}
	if err == nil && len(gvks) > 0 {
		result.kind = gvks[0].Kind
		result.apiVersion = gvks[0].GroupVersion().String()
		result.ok = true
	} else {
		logger.Debug("failed to get GVK for object", "error", err)
	}
	gvkCache.Store(t, result)
	return result
}

func getObjectInfo(obj any) semconv.K8SObjectInfo {
	objInfo := semconv.K8SObjectInfo{}

	if m, err := meta.Accessor(obj); err == nil {
		objInfo.UID = string(m.GetUID())
		objInfo.Name = m.GetName()
		objInfo.Namespace = m.GetNamespace()
	}

	runtimeObj, ok := obj.(runtime.Object)
	if !ok {
		logger.Debug("object does not implement runtime.Object, cannot determine GVK")
		return objInfo
	}

	gvk := lookupGVK(runtimeObj)
	if !gvk.ok {
		return objInfo
	}
	objInfo.Kind = gvk.kind
	objInfo.APIVersion = gvk.apiVersion

	if objInfo.Kind != "Pod" && objInfo.Kind != "HorizontalPodAutoscaler" {
		return objInfo
	}

	switch o := obj.(type) {
	case *corev1.Pod:
		objInfo.NodeName = o.Spec.NodeName
	case *autoscalingv2.HorizontalPodAutoscaler:
		objInfo.HPAScaleTargetRefAPIVersion = o.Spec.ScaleTargetRef.APIVersion
		objInfo.HPAScaleTargetRefKind = o.Spec.ScaleTargetRef.Kind
		objInfo.HPAScaleTargetRefName = o.Spec.ScaleTargetRef.Name
	}

	return objInfo
}
