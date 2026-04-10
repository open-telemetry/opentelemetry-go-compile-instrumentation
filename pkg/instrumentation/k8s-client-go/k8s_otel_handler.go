// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8s_client_go

import (
	"context"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/k8s-client-go/semconv"
	"go.opentelemetry.io/otel/trace"
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
	objInfo := getObjectInfo(obj)
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

	gvks, _, err := scheme.Scheme.ObjectKinds(runtimeObj)
	if err != nil || len(gvks) == 0 {
		logger.Debug("failed to get GVK for object", "error", err)
		return objInfo
	}

	gvk := gvks[0]
	objInfo.Kind = gvk.Kind
	objInfo.APIVersion = gvk.GroupVersion().String()

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
