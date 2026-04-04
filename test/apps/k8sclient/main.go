// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

type PodEvent int

const (
	PodAdded PodEvent = iota
	PodUpdated
	PodDeleted
)

func main() {
	kubeConfigYaml := os.Getenv("KUBECONFIG_YAML")
	if kubeConfigYaml == "" {
		log.Fatal("KUBECONFIG_YAML environment variable is not set")
	}

	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfigYaml))
	if err != nil {
		log.Fatalf("Failed to build kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	stopCh := make(chan struct{})
	podUpdatesCh := make(chan PodEvent)

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		0,
		informers.WithNamespace(corev1.NamespaceDefault),
	)

	podInformer := factory.Core().V1().Pods()
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*corev1.Pod)
			if pod.Name != "test-pod" {
				return
			}
			log.Printf("Added Pod: %s", pod.Name)
			podUpdatesCh <- PodAdded
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*corev1.Pod)
			if pod.Name != "test-pod" {
				return
			}
			log.Printf("Updated Pod: %s", pod.Name)
			podUpdatesCh <- PodUpdated
		},
		DeleteFunc: func(obj any) {
			var pod *corev1.Pod
			switch t := obj.(type) {
			case *corev1.Pod:
				pod = t
			case cache.DeletedFinalStateUnknown:
				pod = t.Obj.(*corev1.Pod)
			}
			if pod.Name != "test-pod" {
				return
			}
			log.Printf("Deleted Pod: %s", pod.Name)
			podUpdatesCh <- PodDeleted
		},
	})

	factory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, podInformer.Informer().HasSynced) {
		log.Fatalf("Failed to wait for caches to sync")
	}

	ctx := context.Background()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: corev1.NamespaceDefault,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "test-container",
					Image:           "registry.k8s.io/pause",
					ImagePullPolicy: corev1.PullNever,
				},
			},
		},
	}

	// create a pod
	_, err = clientset.CoreV1().Pods(corev1.NamespaceDefault).Create(ctx, &pod, metav1.CreateOptions{})
	if err != nil {
		log.Fatalf("Failed to create pod: %v", err)
	}
createLoop:
	for {
		select {
		case ev := <-podUpdatesCh:
			if ev == PodAdded {
				break createLoop
			}
		case <-time.After(10 * time.Second):
			log.Fatalf("Timed out waiting for pod creation event")
		}
	}

	// update the pod
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestPod, err := clientset.CoreV1().Pods(corev1.NamespaceDefault).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		latestPod.Labels = map[string]string{"updated": "true"}
		_, err = clientset.CoreV1().Pods(corev1.NamespaceDefault).Update(ctx, latestPod, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		log.Fatalf("Failed to update pod: %v", err)
	}
updateLoop:
	for {
		select {
		case ev := <-podUpdatesCh:
			if ev == PodUpdated {
				break updateLoop
			}
		case <-time.After(10 * time.Second):
			log.Fatalf("Timed out waiting for pod updation event")
		}
	}

	// delete the pod
	err = clientset.CoreV1().Pods(corev1.NamespaceDefault).Delete(ctx, pod.Name, metav1.DeleteOptions{})
	if err != nil {
		log.Fatalf("Failed to delete pod: %v", err)
	}
deleteLoop:
	for {
		select {
		case ev := <-podUpdatesCh:
			if ev == PodDeleted {
				break deleteLoop
			}
		case <-time.After(10 * time.Second):
			log.Fatalf("Timed out waiting for pod deletion event")
		}
	}

	close(stopCh)
	close(podUpdatesCh)
}
