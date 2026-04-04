// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8s_client_go

import (
	"context"
	"sync"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/k8s-client-go/semconv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, context.TODO())
	assert.NotNil(t, handler)
}

func TestOnAdd(t *testing.T) {
	initOnce = *new(sync.Once)
	sr, _ := setupTestTracer(t)
	initInstrumentation()

	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, context.TODO())
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

	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, context.TODO())
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

	handler := newK8SOtelEventHandler(cache.ResourceEventHandlerFuncs{}, context.TODO())
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
