// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"

	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-call-receiver/helpers/logger"
)

var std = &logger.Logger{}

func getLogger() *logger.Logger { return std }

type server struct{ log *logger.Logger }

// The receiver of the matched call is log, not the enclosing method's s.
func (s *server) serve(log *logger.Logger) {
	log.Info("serve")
	s.log.Info("field")
}

type base struct{ *logger.Logger }

// The receiver renders as written: the outer value of a promoted method, and
// no implicit & for an addressable value.
func promoted(b base, value logger.Logger) {
	b.Info("promoted")
	value.Info("addressable")
}

func handle(log *logger.Logger, fields []logger.Field) {
	defer log.Info("done", logger.String("n", strings.Repeat("x", len(fields))))
	go log.Info("async", fields...)
	getLogger().Info("spread", fields...)
	log.Info("none")
	_ = log.Sync()
}

func main() {
	(&server{log: std}).serve(std)
	handle(std, []logger.Field{logger.String("k", "v")})
	promoted(base{std}, logger.Logger{})
}
