// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8s_client_go

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/instrumentation/k8s.io/client-go/semconv"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

func TestGetSpanName(t *testing.T) {
	for _, tt := range []struct {
		name   string
		action string
		kind   string
		obj    any
	}{
		{
			name:   "test pod add",
			action: "add",
			kind:   "pod",
			obj:    &corev1.Pod{},
		},
		{
			name:   "test deployment update",
			action: "update",
			kind:   "deployment",
			obj:    &appsv1.Deployment{},
		},
		{
			name:   "test nil object delete",
			action: "delete",
			kind:   "object",
			obj:    nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			objInfo := getObjectInfo(tt.obj)
			spanName := getSpanName(objInfo.Kind, tt.action)
			assert.Equal(t, "k8s.informer."+tt.kind+"."+tt.action, spanName)
		})
	}
}

func TestGetObjectInfo(t *testing.T) {
	for _, tt := range []struct {
		name     string
		obj      any
		expected semconv.K8SObjectInfo
	}{
		{
			name: "test basic node",
			obj: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					UID:  "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				},
			},
			expected: semconv.K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-node",
				Kind:       "Node",
				APIVersion: "v1",
			},
		},
		{
			name: "test basic pod",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
					Namespace: corev1.NamespaceDefault,
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
			},
			expected: semconv.K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-pod",
				Namespace:  "default",
				Kind:       "Pod",
				APIVersion: "v1",
				NodeName:   "test-node",
			},
		},
		{
			name: "test basic deployment",
			obj: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
					Namespace: corev1.NamespaceDefault,
				},
			},
			expected: semconv.K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-deployment",
				Namespace:  corev1.NamespaceDefault,
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
		},
		{
			name: "test basic hpa",
			obj: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hpa",
					UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
					Namespace: corev1.NamespaceDefault,
				},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
				},
			},
			expected: semconv.K8SObjectInfo{
				UID:                         "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:                        "test-hpa",
				Namespace:                   corev1.NamespaceDefault,
				Kind:                        "HorizontalPodAutoscaler",
				APIVersion:                  "autoscaling/v2",
				HPAScaleTargetRefAPIVersion: "apps/v1",
				HPAScaleTargetRefKind:       "Deployment",
				HPAScaleTargetRefName:       "test-deployment",
			},
		},
		{
			name:     "test nil object",
			obj:      nil,
			expected: semconv.K8SObjectInfo{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			objInfo := getObjectInfo(tt.obj)
			assert.Equal(t, tt.expected, objInfo)
		})
	}
}

func TestNewK8SOtelHandler(t *testing.T) {
	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, t.Context())
	assert.NotNil(t, handler)
}

func TestOnAdd(t *testing.T) {
	initOnce = *new(sync.Once)
	sr, _ := setupTestTracer(t)
	initInstrumentation()

	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, t.Context())
	handler.OnAdd(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
			Namespace: corev1.NamespaceDefault,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, false)

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "k8s.informer.pod.add", span.Name())

	// Verify attributes
	attrMap := make(map[string]any)
	for _, attr := range span.Attributes() {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "test-pod", attrMap["k8s.pod.name"])
	assert.Equal(t, "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", attrMap["k8s.pod.uid"])
	assert.Equal(t, "default", attrMap["k8s.namespace.name"])
	assert.Equal(t, "test-node", attrMap["k8s.node.name"])
	assert.Equal(t, "v1", attrMap["k8s.object.api_version"])
	assert.Equal(t, "Pod", attrMap["k8s.object.kind"])
}

func TestOnUpdate(t *testing.T) {
	initOnce = *new(sync.Once)
	sr, _ := setupTestTracer(t)
	initInstrumentation()

	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, t.Context())
	handler.OnUpdate(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
			Namespace: corev1.NamespaceDefault,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod2",
			UID:       "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy",
			Namespace: corev1.NamespaceDefault,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node2",
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "k8s.informer.pod.update", span.Name())

	// Verify attributes
	attrMap := make(map[string]any)
	for _, attr := range span.Attributes() {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "test-pod2", attrMap["k8s.pod.name"])
	assert.Equal(t, "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", attrMap["k8s.pod.uid"])
	assert.Equal(t, "default", attrMap["k8s.namespace.name"])
	assert.Equal(t, "test-node2", attrMap["k8s.node.name"])
	assert.Equal(t, "v1", attrMap["k8s.object.api_version"])
	assert.Equal(t, "Pod", attrMap["k8s.object.kind"])
}

func TestOnDelete(t *testing.T) {
	initOnce = *new(sync.Once)
	sr, _ := setupTestTracer(t)
	initInstrumentation()

	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, t.Context())
	handler.OnDelete(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
			Namespace: corev1.NamespaceDefault,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "k8s.informer.pod.delete", span.Name())

	// Verify attributes
	attrMap := make(map[string]any)
	for _, attr := range span.Attributes() {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "test-pod", attrMap["k8s.pod.name"])
	assert.Equal(t, "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", attrMap["k8s.pod.uid"])
	assert.Equal(t, "default", attrMap["k8s.namespace.name"])
	assert.Equal(t, "test-node", attrMap["k8s.node.name"])
	assert.Equal(t, "v1", attrMap["k8s.object.api_version"])
	assert.Equal(t, "Pod", attrMap["k8s.object.kind"])
}

// TestOnDelete_TombstoneForwardsWrappedObject verifies that when client-go
// delivers a DeletedFinalStateUnknown tombstone (a missed watch event during
// a resync, so the delete is inferred rather than confirmed), the wrapper is
// still what reaches the user's own handler. The unwrapped object is only
// used locally to build span attributes; forwarding the unwrapped value
// would silently hide the tombstone from application code that specifically
// checks for it.
func TestOnDelete_TombstoneForwardsWrappedObject(t *testing.T) {
	initOnce = *new(sync.Once)
	sr, _ := setupTestTracer(t)
	initInstrumentation()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
			Namespace: corev1.NamespaceDefault,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}
	tombstone := cache.DeletedFinalStateUnknown{Key: "default/test-pod", Obj: pod}

	var forwarded any
	inner := cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj any) {
			forwarded = obj
		},
	}

	handler := newK8SOtelEventHandler(inner, t.Context())
	handler.OnDelete(tombstone)

	gotTombstone, ok := forwarded.(cache.DeletedFinalStateUnknown)
	require.True(t, ok, "wrapped handler should receive the DeletedFinalStateUnknown wrapper, got %T", forwarded)
	assert.Same(t, pod, gotTombstone.Obj)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "k8s.informer.pod.delete", span.Name())

	attrMap := make(map[string]any)
	for _, attr := range span.Attributes() {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "test-pod", attrMap["k8s.pod.name"], "span attributes should still reflect the unwrapped object")
	assert.Equal(t, "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", attrMap["k8s.pod.uid"])
}

// stubObjectKinds wraps a real ObjectKinds implementation with a call
// counter, so tests can assert the underlying registry scan actually ran
// (or didn't run again), not just that the returned values look right.
func stubObjectKinds(t *testing.T, real func(runtime.Object) ([]schema.GroupVersionKind, bool, error)) (stub func(runtime.Object) ([]schema.GroupVersionKind, bool, error), calls *int) {
	t.Helper()
	n := 0
	return func(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
		n++
		return real(obj)
	}, &n
}

func TestLookupGVK_CachesPerType(t *testing.T) {
	gvkCache = sync.Map{}
	real := objectKindsFunc
	stub, calls := stubObjectKinds(t, real)
	objectKindsFunc = stub
	t.Cleanup(func() { objectKindsFunc = real })

	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a"}}
	pod2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b"}}

	first := lookupGVK(pod1)
	second := lookupGVK(pod2)
	third := lookupGVK(pod1)

	assert.Equal(t, 1, *calls,
		"ObjectKinds should only run once per Go type, not once per event, even across different object instances")
	assert.Equal(t, first, second)
	assert.Equal(t, first, third)
	assert.True(t, first.ok)
	assert.Equal(t, "Pod", first.kind)
	assert.Equal(t, "v1", first.apiVersion)
}

func TestLookupGVK_CachesDifferentTypesSeparately(t *testing.T) {
	gvkCache = sync.Map{}
	real := objectKindsFunc
	stub, calls := stubObjectKinds(t, real)
	objectKindsFunc = stub
	t.Cleanup(func() { objectKindsFunc = real })

	pod := lookupGVK(&corev1.Pod{})
	node := lookupGVK(&corev1.Node{})

	assert.Equal(t, 2, *calls, "two distinct Go types should each trigger their own lookup")
	assert.Equal(t, "Pod", pod.kind)
	assert.Equal(t, "Node", node.kind)
	assert.NotEqual(t, pod, node)
}

func TestLookupGVK_CachesMisses(t *testing.T) {
	gvkCache = sync.Map{}
	real := objectKindsFunc
	stub, calls := stubObjectKinds(t, real)
	objectKindsFunc = stub
	t.Cleanup(func() { objectKindsFunc = real })

	// A type with no GVK registered in the scheme: unregisteredObject is
	// runtime.Object-shaped but never added to scheme.Scheme, so ObjectKinds
	// fails on it every time it's actually called.
	obj := &unregisteredObject{}

	first := lookupGVK(obj)
	second := lookupGVK(obj)

	assert.False(t, first.ok)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, *calls, "a failed lookup should still be cached, not retried on every event")
}

// unregisteredObject implements runtime.Object but is deliberately never
// registered with scheme.Scheme, so ObjectKinds always fails to resolve it.
type unregisteredObject struct {
	metav1.TypeMeta
}

func (o *unregisteredObject) DeepCopyObject() runtime.Object {
	return &unregisteredObject{TypeMeta: o.TypeMeta}
}
