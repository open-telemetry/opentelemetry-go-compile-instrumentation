// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main calls DynamoDB through a client made from an AWS config loaded
// during package initialization.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"example.com/awsinit/early"
)

var endpoint = flag.String("endpoint", "http://localhost:8080", "DynamoDB endpoint URL")

func main() {
	flag.Parse()

	client := dynamodb.NewFromConfig(early.Config, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(*endpoint)
	})
	out, err := client.ListTables(context.Background(), &dynamodb.ListTablesInput{})
	if err != nil {
		log.Fatalf("failed to list tables: %v", err)
	}

	slog.Info("DynamoDB operations completed successfully", "tables", len(out.TableNames))
}
