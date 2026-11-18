// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early sends an HTTP request to a local server during package
// initialization. Its import path sorts before go.opentelemetry.io, so Go
// initializes it before the OpenTelemetry API.
package early

import (
	"net/http"
	"net/http/httptest"
)

func init() {
	if err := Get(); err != nil {
		panic(err)
	}
}

// Get sends one request to a new local server.
func Get() error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
