// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a stripe-go client for integration testing. It is built
// with the otelc compile-time tool and pointed at an in-process mock Stripe API.
//
// The modes deliberately cross both API generations and the create/retrieve/list
// shapes, because all of them are expected to reach the same single hook on the
// backend.
//
// Modes:
//
//	smoke:     v1 create/retrieve/list plus a v2 retrieve (default)
//	not_found: retrieve a missing customer (error-span coverage)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	stripe "github.com/stripe/stripe-go/v82"
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
		API:         stripe.GetBackendWithConfig(stripe.APIBackend, cfg),
		Connect:     stripe.GetBackendWithConfig(stripe.ConnectBackend, cfg),
		Uploads:     stripe.GetBackendWithConfig(stripe.UploadsBackend, cfg),
		MeterEvents: stripe.GetBackendWithConfig(stripe.MeterEventsBackend, cfg),
	}
	client := stripe.NewClient("sk_test_123", stripe.WithBackends(backends))

	ctx := context.Background()

	switch *mode {
	case "not_found":
		runNotFound(ctx, client, *id)
	case "smoke":
		runSmoke(ctx, client, *id)
	default:
		log.Fatalf("unknown mode %q (want smoke|not_found)", *mode)
	}
}

// runSmoke exercises a cross-section of the SDK: a v1 POST, a v1 GET by ID, a
// v1 collection GET, and a v2 GET by ID.
func runSmoke(ctx context.Context, client *stripe.Client, customerID string) {
	created, err := client.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
		Email: stripe.String("test@example.com"),
	})
	if err != nil {
		fail("V1Customers.Create", err)
	}
	slog.Info("create_customer", "id", created.ID)
	fmt.Printf("created customer id=%s\n", created.ID)

	got, err := client.V1Customers.Retrieve(ctx, customerID, nil)
	if err != nil {
		fail("V1Customers.Retrieve", err)
	}
	slog.Info("retrieve_customer", "id", got.ID, "email", got.Email)
	fmt.Printf("customer id=%s email=%s\n", got.ID, got.Email)

	count := 0
	for customer, err := range client.V1Customers.List(ctx, &stripe.CustomerListParams{}) {
		if err != nil {
			fail("V1Customers.List", err)
		}
		_ = customer
		count++
	}
	slog.Info("list_customers", "count", count)
	fmt.Printf("customers count=%d\n", count)

	// A v2 API call on a different backend instance (MeterEvents, not API),
	// which must still reach the same hook.
	meterEvent, err := client.V2BillingMeterEvents.Create(ctx, &stripe.V2BillingMeterEventCreateParams{
		EventName: stripe.String("otelc_test_event"),
		Payload:   map[string]string{"stripe_customer_id": customerID, "value": "1"},
	})
	if err != nil {
		fail("V2BillingMeterEvents.Create", err)
	}
	slog.Info("create_meter_event", "identifier", meterEvent.Identifier)
	fmt.Printf("meter event identifier=%s\n", meterEvent.Identifier)
}

func runNotFound(ctx context.Context, client *stripe.Client, customerID string) {
	_, err := client.V1Customers.Retrieve(ctx, customerID, nil)
	if err != nil {
		// Exit 0 so the process flushes spans; integration tests assert on telemetry.
		fmt.Fprintf(os.Stderr, "Retrieve error: %v\n", err)
		slog.Info("retrieve_customer_error", "id", customerID, "error", err.Error())
		return
	}
	log.Fatalf("expected Retrieve(%q) to fail", customerID)
}

func fail(op string, err error) {
	fmt.Fprintf(os.Stderr, "%s error: %v\n", op, err)
	log.Fatalf("%s failed: %v", op, err)
}
