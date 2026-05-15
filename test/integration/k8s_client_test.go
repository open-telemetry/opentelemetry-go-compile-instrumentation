// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestK8SClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("k3s not supported on windows")
	}

	f := testutil.NewTestFixture(t)
	StartK3sCluster(t)

	output := f.BuildAndRun("k8sclient")
	require.Contains(t, output, "Added Pod")

	spans := testutil.AllSpans(f.Traces())
	require.GreaterOrEqual(t, len(spans), 3, "expected at least 3 spans (Create Pod, Update Pod, Delete Pod)")

	// Verify Create span
	createSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsInternal,
		testutil.HasName("k8s.informer.pod.add"),
	)
	testutil.RequireK8SClientSemconv(
		t,
		createSpan,
		"test-pod",
		false,
	)

	// Verify Update Span
	updateSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsInternal,
		testutil.HasName("k8s.informer.pod.update"),
		testutil.AttributeExists("k8s.node.name"),
	)
	testutil.RequireK8SClientSemconv(
		t,
		updateSpan,
		"test-pod",
		true,
	)

	// Verify Delete Span
	deleteSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsInternal,
		testutil.HasName("k8s.informer.pod.delete"),
	)
	testutil.RequireK8SClientSemconv(
		t,
		deleteSpan,
		"test-pod",
		true,
	)
}

func StartK3sCluster(t *testing.T) {
	ctx := context.Background()

	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.27.1-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kubeConfigYaml, err := k3sContainer.GetKubeConfig(ctx)
	require.NoError(t, err)
	t.Setenv("KUBECONFIG_YAML", string(kubeConfigYaml))

	provider, err := testcontainers.ProviderDocker.GetProvider()
	require.NoError(t, err)

	err = provider.PullImage(ctx, "registry.k8s.io/pause")
	require.NoError(t, err)

	err = k3sContainer.LoadImages(ctx, "registry.k8s.io/pause")
	require.NoError(t, err)
}
