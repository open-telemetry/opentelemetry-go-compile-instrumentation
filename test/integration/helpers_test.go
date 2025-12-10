// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitForServerReady waits for a server to be ready by monitoring its output for "server started".
// It returns:
//   - func() string: a cleanup function that waits for the server to exit and returns its complete output
//   - error: non-nil if the server failed to start within the timeout
//
// This helper provides better error messages on timeout by including the server's output.
// Callers should check the error and use require.NoError to fail the test with proper context.
func waitForServerReady(t *testing.T, serverCmd *exec.Cmd, output io.ReadCloser) (func() string, error) {
	t.Helper()

	readyChan := make(chan struct{})
	doneChan := make(chan struct{})
	outputBuilder := strings.Builder{}
	const readyMsg = "server started"

	// Use mutex to safely access outputBuilder from timeout handler
	var mu sync.Mutex

	go func() {
		defer close(doneChan)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			outputBuilder.WriteString(line + "\n")
			mu.Unlock()
			if strings.Contains(line, readyMsg) {
				close(readyChan)
			}
		}
	}()

	waitUntilDone := func() string {
		_ = serverCmd.Wait()
		<-doneChan
		mu.Lock()
		defer mu.Unlock()
		return outputBuilder.String()
	}

	select {
	case <-readyChan:
		t.Logf("Server is ready!")
		return waitUntilDone, nil
	case <-time.After(15 * time.Second):
		mu.Lock()
		serverOutput := outputBuilder.String()
		mu.Unlock()
		if serverOutput == "" {
			return nil, errors.New("timeout waiting for server to be ready - no server output received")
		}
		return nil, fmt.Errorf("timeout waiting for server to be ready - server output:\n%s", serverOutput)
	}
}
