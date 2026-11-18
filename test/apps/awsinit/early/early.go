// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early loads an AWS config in a package-level variable. Its import
// path sorts before go.opentelemetry.io, so Go initializes it before the
// OpenTelemetry API, and the config.LoadDefaultConfig hook runs that early.
package early

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// Config is loaded during package initialization.
var Config = loadConfig()

func loadConfig() aws.Config {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		panic(err)
	}
	return cfg
}
