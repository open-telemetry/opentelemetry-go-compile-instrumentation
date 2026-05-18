// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/databasesql/internal/dsnparse"
)

// DSNParser parses a driver-specific DSN and returns the server address (host:port).
type DSNParser = dsnparse.DSNParser

// RegisterDSNParser registers a custom DSN parser for the given driver name.
// Built-in parsers are registered automatically during package initialization.
// Calling RegisterDSNParser for an already-registered name overwrites the previous parser.
// It is safe to call from package init() functions.
func RegisterDSNParser(driverName string, parser dsnparse.DSNParser) {
	dsnparse.RegisterDSNParser(driverName, parser)
}

func parseDSN(driverName, dsn string) (string, error) {
	return dsnparse.ParseDSN(driverName, dsn)
}

func parseDbName(dsn string) string {
	return dsnparse.ParseDbName(dsn)
}
