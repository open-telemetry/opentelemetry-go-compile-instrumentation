// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otelc/test/testutil"
)

const awsMockRequestID = "test-request-id-123"

func TestAWSClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "awsclient", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	server := startMockDynamoDBServer(t)

	f.Run("awsclient", fmt.Sprintf("-endpoint=%s", server.URL))

	span := testutil.RequireSpan(t, f.Traces(),
		testutil.HasName("DynamoDB.ListTables"),
		testutil.IsClient,
	)
	testutil.RequireAWSClientSemconv(t, span, awsMockRequestID)
}

// startMockDynamoDBServer creates a mock DynamoDB endpoint for testing.
func startMockDynamoDBServer(t *testing.T) *httptest.Server {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") != "DynamoDB_20120810.ListTables" {
			http.Error(w, "unexpected target", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		w.Header().Set("X-Amzn-Requestid", awsMockRequestID)
		if _, err := w.Write([]byte(`{"TableNames":["users"]}`)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}
