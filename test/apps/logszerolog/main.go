// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"
	zerologlog "github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type ErrWriter struct {
	w io.Writer
}

func (e *ErrWriter) Write(p []byte) (n int, err error) {
	return e.w.Write(p)
}

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

	zerolog.SetGlobalLevel(zerolog.TraceLevel)

	errWriter := &ErrWriter{w: os.Stderr}
	zerologlog.Logger = zerologlog.Logger.Output(errWriter).Level(zerolog.TraceLevel)
	instantiated := zerolog.Logger{}.Output(errWriter).Level(zerolog.TraceLevel)

	emitEvents(&zerologlog.Logger, "loggerFromNew")
	emitEvents(&instantiated, "loggerFromInstantiated")

	span.End()
}

func emitEvents(logger *zerolog.Logger, appended string) {
	logger.Trace().Msgf("trace message %s", appended)
	logger.Debug().Msgf("debug message %s", appended)
	logger.Info().Msgf("info message %s", appended)
	logger.Warn().Msgf("warn message %s", appended)
	logger.Error().Msgf("error message %s", appended)
	logger.Err(errors.New("test error")).Msgf("err with message %s", appended)
	logger.Log().Msgf("log message %s", appended)
	logger.Print(fmt.Sprintf("logger print message %s", appended))
	logger.Printf("logger printf message %s", appended)
	logger.Println(fmt.Sprintf("logger println message %s", appended))

	previousFatalExitFunc := zerolog.FatalExitFunc
	zerolog.FatalExitFunc = func() {}
	defer func() {
		zerolog.FatalExitFunc = previousFatalExitFunc
	}()
	func() {
		defer func() {
			_ = recover()
		}()
		logger.Panic().Msgf("panic message %s", appended)
	}()
	logger.Fatal().Msgf("fatal message %s", appended)
}
