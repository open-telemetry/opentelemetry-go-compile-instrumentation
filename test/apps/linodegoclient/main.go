// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a multi-resource linodego client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
//
// Modes:
//
//	smoke     — several read APIs across resource types (default)
//	not_found — GetInstance against a missing ID (error-span coverage)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/linode/linodego/v2"
)

var (
	addr = flag.String("addr", "http://localhost:8080", "Base URL of the mock Linode API (scheme://host[:port])")
	mode = flag.String("mode", "smoke", "Test mode: smoke | not_found")
	id   = flag.Int("id", 123, "Resource ID used by get operations")
)

func main() {
	flag.Parse()

	client, err := linodego.NewClient(http.DefaultClient)
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}
	client.SetToken("test-token")
	// Avoid response cache so each ListRegions call hits the wire in tests.
	client.UseCache(false)

	apiRoot := *addr + "/v4"
	if _, err := client.UseURL(apiRoot); err != nil {
		log.Fatalf("failed to set API URL %q: %v", apiRoot, err)
	}

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

// runSmoke exercises a cross-section of the Linode API client surface:
// regions (catalog), account (billing/contact), volumes (block storage),
// and instances (compute) — both list and get where applicable.
func runSmoke(ctx context.Context, client linodego.Client, resourceID int) {
	// 1. Catalog: list regions
	regions, err := client.ListRegions(ctx, nil)
	if err != nil {
		fail("ListRegions", err)
	}
	slog.Info("list_regions", "count", len(regions))
	fmt.Printf("regions count=%d\n", len(regions))
	if len(regions) > 0 {
		fmt.Printf("region id=%s label=%s\n", regions[0].ID, regions[0].Label)
	}

	// 2. Account: get account profile
	account, err := client.GetAccount(ctx)
	if err != nil {
		fail("GetAccount", err)
	}
	slog.Info("get_account", "email", account.Email, "company", account.Company)
	fmt.Printf("account email=%s company=%s\n", account.Email, account.Company)

	// 3. Volumes: list + get
	volumes, err := client.ListVolumes(ctx, nil)
	if err != nil {
		fail("ListVolumes", err)
	}
	slog.Info("list_volumes", "count", len(volumes))
	fmt.Printf("volumes count=%d\n", len(volumes))

	vol, err := client.GetVolume(ctx, resourceID)
	if err != nil {
		fail("GetVolume", err)
	}
	slog.Info("get_volume", "id", vol.ID, "label", vol.Label)
	fmt.Printf("volume id=%d label=%s\n", vol.ID, vol.Label)

	// 4. Instances: list + get
	instances, err := client.ListInstances(ctx, nil)
	if err != nil {
		fail("ListInstances", err)
	}
	slog.Info("list_instances", "count", len(instances))
	fmt.Printf("instances count=%d\n", len(instances))

	inst, err := client.GetInstance(ctx, resourceID)
	if err != nil {
		fail("GetInstance", err)
	}
	slog.Info("get_instance", "id", inst.ID, "label", inst.Label, "region", inst.Region)
	fmt.Printf("instance id=%d label=%s\n", inst.ID, inst.Label)
}

func runNotFound(ctx context.Context, client linodego.Client, resourceID int) {
	_, err := client.GetInstance(ctx, resourceID)
	if err != nil {
		// Exit 0 so the process flushes spans; integration tests assert on telemetry.
		fmt.Fprintf(os.Stderr, "GetInstance error: %v\n", err)
		slog.Info("get_instance_error", "id", resourceID, "error", err.Error())
		return
	}
	log.Fatalf("expected GetInstance(%d) to fail", resourceID)
}

func fail(op string, err error) {
	fmt.Fprintf(os.Stderr, "%s error: %v\n", op, err)
	log.Fatalf("%s failed: %v", op, err)
}
