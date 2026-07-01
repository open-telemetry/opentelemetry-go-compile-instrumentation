// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os/exec"
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

	// Check if docker is available and running
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker is not available or not running, skipping k3s integration test")
	}

	t.Parallel()
	testutil.Build(t, "", "k8sclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	StartK3sCluster(t, f)

	output := f.Run("k8sclient")
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

func StartK3sCluster(t *testing.T, f *testutil.TestFixture) {
	k3sContainer, err := k3s.Run(t.Context(), "rancher/k3s:v1.27.1-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	kubeConfigYaml, err := k3sContainer.GetKubeConfig(t.Context())
	require.NoError(t, err)
	f.SetEnv("KUBECONFIG_YAML", string(kubeConfigYaml))

	provider, err := testcontainers.ProviderDocker.GetProvider()
	require.NoError(t, err)

	err = provider.PullImage(t.Context(), "registry.k8s.io/pause")
	require.NoError(t, err)

	err = k3sContainer.LoadImages(t.Context(), "registry.k8s.io/pause")
	require.NoError(t, err)
}
