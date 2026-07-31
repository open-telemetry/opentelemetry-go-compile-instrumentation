// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	// K8SObjectUID is a generic attribute for Kubernetes object UID, used when specific UID attributes are not available for the object kind
	// Type: string
	// Example: "123e4567-e89b-12d3-a456-426614174000"
	K8SObjectUID = "k8s.object.uid"

	// K8SObjectName is a generic attribute for Kubernetes object name, used when specific name attributes are not available for the object kind
	// Type: string
	// Example: "my-pod-abc123"
	K8SObjectName = "k8s.object.name"

	// K8SObjectAPIVersion represents the API version of the Kubernetes object
	// Type: string
	// Example: "apps/v1"
	K8SObjectAPIVersion = "k8s.object.api_version"

	// K8SObjectKind represents the kind of the Kubernetes object
	// Type: string
	// Example: "Pod"
	K8SObjectKind = "k8s.object.kind"

	// K8SObjectsCount represents the number of Kubernetes objects involved in an operation
	// Type: int
	// Example: 3
	K8SObjectsCount = "k8s.objects.count"

	// K8SObjectsIsInInitialList indicates whether the Kubernetes objects were from the initial list during informer startup
	// Type: bool
	// Example: true
	K8SObjectsIsInInitialList = "k8s.objects.is_in_initial_list"
)

type K8SObjectInfo struct {
	UID        string
	Name       string
	Namespace  string
	Kind       string
	APIVersion string

	// optional: for pods
	NodeName string

	// optional: for HPAs
	HPAScaleTargetRefAPIVersion string
	HPAScaleTargetRefKind       string
	HPAScaleTargetRefName       string
}

type K8SObjectsInfo struct {
	Count           int
	IsInInitialList bool
}

// K8SObjectInfoTraceAttrs returns trace attributes for K8SObjectInfo
func K8SObjectInfoTraceAttrs(objInfo K8SObjectInfo) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}

	switch objInfo.Kind {
	case "Node":
		attrs = append(attrs,
			semconv.K8SNodeUID(objInfo.UID),
			semconv.K8SNodeName(objInfo.Name),
		)
	case "Namespace":
		attrs = append(attrs,
			semconv.K8SNamespaceName(objInfo.Name),
		)
	case "Pod":
		attrs = append(attrs,
			semconv.K8SPodUID(objInfo.UID),
			semconv.K8SPodName(objInfo.Name),
		)
		if len(objInfo.NodeName) > 0 {
			attrs = append(attrs,
				semconv.K8SNodeName(objInfo.NodeName),
			)
		}
	case "ReplicaSet":
		attrs = append(attrs,
			semconv.K8SReplicaSetUID(objInfo.UID),
			semconv.K8SReplicaSetName(objInfo.Name),
		)
	case "Deployment":
		attrs = append(attrs,
			semconv.K8SDeploymentUID(objInfo.UID),
			semconv.K8SDeploymentName(objInfo.Name),
		)
	case "StatefulSet":
		attrs = append(attrs,
			semconv.K8SStatefulSetUID(objInfo.UID),
			semconv.K8SStatefulSetName(objInfo.Name),
		)
	case "DaemonSet":
		attrs = append(attrs,
			semconv.K8SDaemonSetUID(objInfo.UID),
			semconv.K8SDaemonSetName(objInfo.Name),
		)
	case "Job":
		attrs = append(attrs,
			semconv.K8SJobUID(objInfo.UID),
			semconv.K8SJobName(objInfo.Name),
		)
	case "CronJob":
		attrs = append(attrs,
			semconv.K8SCronJobUID(objInfo.UID),
			semconv.K8SCronJobName(objInfo.Name),
		)
	case "ReplicationController":
		attrs = append(attrs,
			semconv.K8SReplicationControllerUID(objInfo.UID),
			semconv.K8SReplicationControllerName(objInfo.Name),
		)
	case "HorizontalPodAutoscaler":
		attrs = append(attrs,
			semconv.K8SHPAUID(objInfo.UID),
			semconv.K8SHPAName(objInfo.Name),
		)
		if len(objInfo.HPAScaleTargetRefAPIVersion) > 0 && len(objInfo.HPAScaleTargetRefKind) > 0 &&
			len(objInfo.HPAScaleTargetRefName) > 0 {
			attrs = append(
				attrs,
				semconv.K8SHPAScaletargetrefAPIVersion(objInfo.HPAScaleTargetRefAPIVersion),
				semconv.K8SHPAScaletargetrefKind(objInfo.HPAScaleTargetRefKind),
				semconv.K8SHPAScaletargetrefName(objInfo.HPAScaleTargetRefName),
			)
		}
	case "ResourceQuota":
		attrs = append(attrs,
			semconv.K8SResourceQuotaUID(objInfo.UID),
			semconv.K8SResourceQuotaName(objInfo.Name),
		)
	default:
		if len(objInfo.UID) > 0 {
			attrs = append(attrs,
				attribute.String(K8SObjectUID, objInfo.UID),
			)
		}
		if len(objInfo.Name) > 0 {
			attrs = append(attrs,
				attribute.String(K8SObjectName, objInfo.Name),
			)
		}
	}

	if len(objInfo.Namespace) > 0 {
		attrs = append(attrs,
			semconv.K8SNamespaceName(objInfo.Namespace),
		)
	}

	if len(objInfo.APIVersion) > 0 && len(objInfo.Kind) > 0 {
		attrs = append(attrs,
			attribute.String(K8SObjectAPIVersion, objInfo.APIVersion),
			attribute.String(K8SObjectKind, objInfo.Kind),
		)
	}

	return attrs
}

// K8SObjectsInfoTraceAttrs returns trace attributes for K8SObjectsInfo
func K8SObjectsInfoTraceAttrs(objsInfo K8SObjectsInfo) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int(K8SObjectsCount, objsInfo.Count),
		attribute.Bool(K8SObjectsIsInInitialList, objsInfo.IsInInitialList),
	}
}
