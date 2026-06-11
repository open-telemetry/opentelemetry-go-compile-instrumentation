// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/databasesql/dsnparse"
)

// DSNInfo is an alias for dsnparse.DSNInfo so callers that import the db
// package can use the type without an additional import.
type DSNInfo = dsnparse.DSNInfo

// ParseDSN parses a driver-specific data source name and returns structured
// connection information. It tries multiple well-known formats in order and
// never panics. Unrecognised drivers return a zero-value DSNInfo.
func ParseDSN(driverName, dsn string) DSNInfo {
	return dsnparse.ParseDSN(driverName, dsn)
}

// ParseDbName extracts the database name from a generic DSN by finding the
// last '/' and trimming any query-string suffix. Retained for backward
// compatibility; prefer ParseDSN when the driver name is known.
func ParseDbName(dsn string) string {
	return dsnparse.ParseDbName(dsn)
}

// parseDSN is the package-internal adapter called by beforeOpenInstrumentation.
func parseDSN(driverName, dsn string) (string, error) {
	return dsnparse.LegacyParseDSN(driverName, dsn)
}
