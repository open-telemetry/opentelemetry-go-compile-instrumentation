// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"time"

	"go.uber.org/zap"
)

func main() {
	defer func() {
		// Wait for OpenTelemetry SDK to flush logs before exit
		time.Sleep(2 * time.Second)
	}()

	ctx := context.Background()

	logger, err := zap.NewProductionConfig().Build()
	if err != nil {
		panic(err)
	}

	logger.With(zap.Any("ctx", ctx)).
		With(zap.Int32("id", 12345)).
		Info("hello world!")
}
