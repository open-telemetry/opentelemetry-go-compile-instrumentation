// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package applog creates its logger in a package-level variable initializer.
// Its import path sorts before go.opentelemetry.io, so Go initializes it
// before the logrus hook package, and the logrus.New hook runs early.
package applog

import "github.com/sirupsen/logrus"

var Logger = newLogger()

func newLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	return logger
}
