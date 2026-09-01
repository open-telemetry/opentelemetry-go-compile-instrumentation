// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	zerologlog "github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	exporter, err := stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create exporter: %v\n", err)
		os.Exit(1)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	otel.SetTracerProvider(tp)

	tracer := otel.GetTracerProvider().Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")

	emitEvents(&zerologlog.Logger)

	span.End()
}

func emitEvents(logger *zerolog.Logger) {
	logger.Trace().Msg("trace message")
	logger.Debug().Msg("debug message")
	logger.Info().Msg("info message")
	logger.Warn().Msg("warn message")
	logger.Error().Msg("error message")
	logger.Err(errors.New("test error")).Msg("err with message")
	logger.Log().Msg("log message")
	logger.Print("logger print message")
	logger.Printf("logger printf message")
	logger.Println("logger println message")

	previousFatalExitFunc := zerolog.FatalExitFunc
	zerolog.FatalExitFunc = func() {}
	defer func() {
		zerolog.FatalExitFunc = previousFatalExitFunc
	}()
	func() {
		defer func() {
			_ = recover()
		}()
		logger.Panic().Msg("panic message")
	}()
	logger.Fatal().Msg("fatal message")
}
