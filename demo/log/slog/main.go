package main

import (
	"context"
	"log/slog"
	"time"
)

func main() {
	defer func() {
		// Wait for OpenTelemetry SDK to flush logs before exit
		time.Sleep(2 * time.Second)
	}()

	ctx := context.Background()

	logger := slog.Default()
	logger.With("id", 12345).InfoContext(ctx, "hello world!")
}
