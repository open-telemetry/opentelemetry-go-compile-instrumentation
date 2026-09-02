// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a stripe-go v81 client for integration testing. It is
// built with the otelc compile-time tool and pointed at an in-process mock
// Stripe API.
//
// v81 predates stripe.Client and the v2 API surface, so this app drives the
// client.API aggregate instead. That is the point: a different client surface
// from the v82 app must still reach the same single hook on the backend.
//
// Modes:
//
//	smoke:     create/retrieve/list customers (default)
//	not_found: retrieve a missing customer (error-span coverage)
package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
)

var (
	addr = flag.String("addr", "http://localhost:8080", "Base URL of the mock Stripe API (scheme://host[:port])")
	mode = flag.String("mode", "smoke", "Test mode: smoke | not_found")
	id   = flag.String("id", "cus_NffrFeUfNV2Hib", "Customer ID used by retrieve operations")
)

func main() {
	flag.Parse()

	// Point every backend at the mock and disable retries, so span counts in the
	// integration test are deterministic.
	cfg := &stripe.BackendConfig{
		URL:               addr,
		MaxNetworkRetries: stripe.Int64(0),
	}
	backends := &stripe.Backends{
		API:     stripe.GetBackendWithConfig(stripe.APIBackend, cfg),
		Connect: stripe.GetBackendWithConfig(stripe.ConnectBackend, cfg),
		Uploads: stripe.GetBackendWithConfig(stripe.UploadsBackend, cfg),
	}
	api := client.New("sk_test_123", backends)

	switch *mode {
	case "not_found":
		runNotFound(api, *id)
	case "smoke":
		runSmoke(api, *id)
	default:
		log.Fatalf("unknown mode %q (want smoke|not_found)", *mode)
	}
}

// runSmoke exercises a POST, a GET by ID and a collection GET.
func runSmoke(api *client.API, customerID string) {
	created, err := api.Customers.New(&stripe.CustomerParams{
		Email: stripe.String("test@example.com"),
	})
	if err != nil {
		fail("Customers.New", err)
	}
	slog.Info("create_customer", "id", created.ID)
	fmt.Printf("created customer id=%s\n", created.ID)

	got, err := api.Customers.Get(customerID, nil)
	if err != nil {
		fail("Customers.Get", err)
	}
	slog.Info("retrieve_customer", "id", got.ID, "email", got.Email)
	fmt.Printf("customer id=%s email=%s\n", got.ID, got.Email)

	count := 0
	iter := api.Customers.List(&stripe.CustomerListParams{})
	for iter.Next() {
		count++
	}
	if err := iter.Err(); err != nil {
		fail("Customers.List", err)
	}
	slog.Info("list_customers", "count", count)
	fmt.Printf("customers count=%d\n", count)
}

func runNotFound(api *client.API, customerID string) {
	_, err := api.Customers.Get(customerID, nil)
	if err != nil {
		// Exit 0 so the process flushes spans; integration tests assert on telemetry.
		fmt.Fprintf(os.Stderr, "Get error: %v\n", err)
		slog.Info("retrieve_customer_error", "id", customerID, "error", err.Error())
		return
	}
	log.Fatalf("expected Get(%q) to fail", customerID)
}

func fail(op string, err error) {
	fmt.Fprintf(os.Stderr, "%s error: %v\n", op, err)
	log.Fatalf("%s failed: %v", op, err)
}
