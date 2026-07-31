// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestK8SObjectInfoTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		objInfo  K8SObjectInfo
		expected map[string]any
	}{
		{
			name: "basic pod",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-pod",
				Namespace:  "default",
				Kind:       "Pod",
				APIVersion: "v1",
				NodeName:   "test-node",
			},
			expected: map[string]any{
				"k8s.pod.uid":            "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.pod.name":           "test-pod",
				"k8s.namespace.name":     "default",
				"k8s.node.name":          "test-node",
				"k8s.object.api_version": "v1",
				"k8s.object.kind":        "Pod",
			},
		},
		{
			name: "basic pod, no node name",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-pod",
				Namespace:  "default",
				Kind:       "Pod",
				APIVersion: "v1",
			},
			expected: map[string]any{
				"k8s.pod.uid":            "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.pod.name":           "test-pod",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "v1",
				"k8s.object.kind":        "Pod",
			},
		},
		{
			name: "basic pod, no namespace",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-pod",
				Kind:       "Pod",
				APIVersion: "v1",
				NodeName:   "test-node",
			},
			expected: map[string]any{
				"k8s.pod.uid":            "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.pod.name":           "test-pod",
				"k8s.node.name":          "test-node",
				"k8s.object.api_version": "v1",
				"k8s.object.kind":        "Pod",
			},
		},
		{
			name: "basic node",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-node",
				Kind:       "Node",
				APIVersion: "v1",
			},
			expected: map[string]any{
				"k8s.node.uid":           "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.node.name":          "test-node",
				"k8s.object.api_version": "v1",
				"k8s.object.kind":        "Node",
			},
		},
		{
			name: "basic namespace",
			objInfo: K8SObjectInfo{
				Name:       "test-namespace",
				Kind:       "Namespace",
				APIVersion: "v1",
			},
			expected: map[string]any{
				"k8s.namespace.name":     "test-namespace",
				"k8s.object.api_version": "v1",
				"k8s.object.kind":        "Namespace",
			},
		},
		{
			name: "basic replicaset",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-replicaset",
				Namespace:  "default",
				Kind:       "ReplicaSet",
				APIVersion: "apps/v1",
			},
			expected: map[string]any{
				"k8s.replicaset.uid":     "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.replicaset.name":    "test-replicaset",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "apps/v1",
				"k8s.object.kind":        "ReplicaSet",
			},
		},
		{
			name: "basic deployment",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-deployment",
				Namespace:  "default",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			expected: map[string]any{
				"k8s.deployment.uid":     "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.deployment.name":    "test-deployment",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "apps/v1",
				"k8s.object.kind":        "Deployment",
			},
		},
		{
			name: "basic statefulset",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-statefulset",
				Namespace:  "default",
				Kind:       "StatefulSet",
				APIVersion: "apps/v1",
			},
			expected: map[string]any{
				"k8s.statefulset.uid":    "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.statefulset.name":   "test-statefulset",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "apps/v1",
				"k8s.object.kind":        "StatefulSet",
			},
		},
		{
			name: "basic daemonset",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-daemonset",
				Namespace:  "default",
				Kind:       "DaemonSet",
				APIVersion: "apps/v1",
			},
			expected: map[string]any{
				"k8s.daemonset.uid":      "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.daemonset.name":     "test-daemonset",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "apps/v1",
				"k8s.object.kind":        "DaemonSet",
			},
		},
		{
			name: "basic job",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-job",
				Namespace:  "default",
				Kind:       "Job",
				APIVersion: "batch/v1",
			},
			expected: map[string]any{
				"k8s.job.uid":            "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.job.name":           "test-job",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "batch/v1",
				"k8s.object.kind":        "Job",
			},
		},
		{
			name: "basic cron job",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-cronjob",
				Namespace:  "default",
				Kind:       "CronJob",
				APIVersion: "batch/v1",
			},
			expected: map[string]any{
				"k8s.cronjob.uid":        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.cronjob.name":       "test-cronjob",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "batch/v1",
				"k8s.object.kind":        "CronJob",
			},
		},
		{
			name: "basic replication controller",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-replicationcontroller",
				Namespace:  "default",
				Kind:       "ReplicationController",
				APIVersion: "v1",
			},
			expected: map[string]any{
				"k8s.replicationcontroller.uid":  "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.replicationcontroller.name": "test-replicationcontroller",
				"k8s.namespace.name":             "default",
				"k8s.object.api_version":         "v1",
				"k8s.object.kind":                "ReplicationController",
			},
		},
		{
			name: "basic hpa",
			objInfo: K8SObjectInfo{
				UID:                         "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:                        "test-hpa",
				Namespace:                   "default",
				Kind:                        "HorizontalPodAutoscaler",
				APIVersion:                  "autoscaling/v2",
				HPAScaleTargetRefAPIVersion: "apps/v1",
				HPAScaleTargetRefKind:       "Deployment",
				HPAScaleTargetRefName:       "test-deployment",
			},
			expected: map[string]any{
				"k8s.hpa.uid":                        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.hpa.name":                       "test-hpa",
				"k8s.namespace.name":                 "default",
				"k8s.hpa.scaletargetref.api_version": "apps/v1",
				"k8s.hpa.scaletargetref.kind":        "Deployment",
				"k8s.hpa.scaletargetref.name":        "test-deployment",
				"k8s.object.api_version":             "autoscaling/v2",
				"k8s.object.kind":                    "HorizontalPodAutoscaler",
			},
		},
		{
			name: "basic resource quota",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-resourcequota",
				Namespace:  "default",
				Kind:       "ResourceQuota",
				APIVersion: "v1",
			},
			expected: map[string]any{
				"k8s.resourcequota.uid":  "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.resourcequota.name": "test-resourcequota",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "v1",
				"k8s.object.kind":        "ResourceQuota",
			},
		},
		{
			name: "basic unknown type",
			objInfo: K8SObjectInfo{
				UID:       "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:      "test-unknown",
				Namespace: "default",
			},
			expected: map[string]any{
				"k8s.object.uid":     "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.object.name":    "test-unknown",
				"k8s.namespace.name": "default",
			},
		},
		{
			name: "basic unknown type with api version and kind",
			objInfo: K8SObjectInfo{
				UID:        "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				Name:       "test-unknown",
				Namespace:  "default",
				Kind:       "UnknownKind",
				APIVersion: "example.com/v1",
			},
			expected: map[string]any{
				"k8s.object.uid":         "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"k8s.object.name":        "test-unknown",
				"k8s.namespace.name":     "default",
				"k8s.object.api_version": "example.com/v1",
				"k8s.object.kind":        "UnknownKind",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := K8SObjectInfoTraceAttrs(tt.objInfo)

			attrMap := make(map[string]any)
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			require.Len(t, attrMap, len(tt.expected), "attribute count mismatch")

			for key, expectedVal := range tt.expected {
				actualVal, ok := attrMap[key]
				require.True(t, ok, "expected attribute %s not found", key)
				assert.Equal(t, expectedVal, actualVal, "attribute %s value mismatch", key)
			}
		})
	}
}

func TestK8SObjectsInfoTraceAttrs(t *testing.T) {
	tests := []struct {
		name     string
		objsInfo K8SObjectsInfo
		expected map[string]any
	}{
		{
			name: "basic pod",
			objsInfo: K8SObjectsInfo{
				Count:           42,
				IsInInitialList: false,
			},
			expected: map[string]any{
				"k8s.objects.count":              int64(42),
				"k8s.objects.is_in_initial_list": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := K8SObjectsInfoTraceAttrs(tt.objsInfo)

			attrMap := make(map[string]any)
			for _, attr := range attrs {
				attrMap[string(attr.Key)] = attr.Value.AsInterface()
			}

			require.Len(t, attrMap, len(tt.expected), "attribute count mismatch")

			for key, expectedVal := range tt.expected {
				actualVal, ok := attrMap[key]
				require.True(t, ok, "expected attribute %s not found", key)
				assert.Equal(t, expectedVal, actualVal, "attribute %s value mismatch", key)
			}
		})
	}
}
