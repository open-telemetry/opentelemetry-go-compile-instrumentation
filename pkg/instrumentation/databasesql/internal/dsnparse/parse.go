// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dsnparse

import (
	"errors"
	"fmt"
	nurl "net/url"
	"strings"
	"sync"
)

// DSNParser parses a driver-specific DSN and returns the server address (host:port).
type DSNParser func(dsn string) (addr string, err error)

var (
	parserMu       sync.RWMutex
	parserRegistry = map[string]DSNParser{}
)

// RegisterDSNParser registers a custom DSN parser for the given driver name.
// Built-in parsers are registered automatically during package initialization.
// Calling RegisterDSNParser for an already-registered name overwrites the previous parser.
// It is safe to call from package init() functions.
func RegisterDSNParser(driverName string, parser DSNParser) {
	parserMu.Lock()
	defer parserMu.Unlock()
	parserRegistry[driverName] = parser
}

func init() {
	// Register all built-in DSN parsers.
	RegisterDSNParser("mysql", ParseMySQL)
	RegisterDSNParser("postgres", ParsePostgres)
	RegisterDSNParser("postgresql", ParsePostgres)
	RegisterDSNParser("pgx", ParsePostgres)    // pgx uses the standard postgres URL format
	RegisterDSNParser("lib/pq", ParsePostgres) // lib/pq uses the standard postgres URL format
	RegisterDSNParser("clickhouse", ParseClickHouse)
	RegisterDSNParser("sqlite3", func(_ string) (string, error) { return "sqlite3", nil })
	RegisterDSNParser("godror", ParseOracle)
	RegisterDSNParser("oracle", ParseOracle)
	RegisterDSNParser("oci8", ParseOracle)
	RegisterDSNParser("go-oci8", ParseOracle)
	RegisterDSNParser("mssql", ParseSQLServer)
	RegisterDSNParser("sqlserver", ParseSQLServer)
}

// ParseDSN parses driverName and dsn into a server address (host:port).
// It falls back to best-effort URL parsing for unregistered drivers.
func ParseDSN(driverName, dsn string) (addr string, err error) {
	parserMu.RLock()
	parser, ok := parserRegistry[driverName]
	parserMu.RUnlock()

	if ok {
		return parser(dsn)
	}

	// Best-effort: try standard URL parsing for drivers not in the registry.
	return BestEffortParse(dsn)
}

// BestEffortParse attempts to extract a host:port from a DSN using standard URL parsing.
// It is used as a fallback for drivers that have no registered parser.
func BestEffortParse(dsn string) (string, error) {
	u, err := nurl.Parse(dsn)
	if err == nil && u.Host != "" {
		return u.Host, nil
	}
	return "", errors.New("no DSN parser registered for this driver; best-effort URL parse also failed")
}

// ParseDbName extracts the database name from a DSN using best-effort URL parsing.
// For URL-based DSNs it uses the path component; for MySQL-style DSNs it uses
// the segment between the closing parenthesis and the first '?'. Returns an
// empty string when the name cannot be determined.
func ParseDbName(dsn string) string {
	// MySQL style: user:pass@tcp(host:port)/dbname?params
	if i := strings.LastIndex(dsn, ")/"); i >= 0 {
		rest := dsn[i+2:]
		if j := strings.IndexByte(rest, '?'); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	// URL style: scheme://user:pass@host/dbname?params
	u, err := nurl.Parse(dsn)
	if err == nil && u.Scheme != "" && u.Path != "" {
		return strings.TrimPrefix(u.Path, "/")
	}
	return ""
}

func ParsePostgres(url string) (addr string, err error) {
	u, err := nurl.Parse(url)
	if err != nil {
		return "", err
	}

	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("invalid connection protocol: %s", u.Scheme)
	}

	if u.Port() != "" {
		return u.Host, nil
	}
	return u.Hostname() + ":5432", nil
}

func ParseMySQL(dsn string) (addr string, err error) {
	// MySQL DSN format: [username[:password]@][protocol[(address)]]/dbname[?params]
	// We need to find the protocol part after @ to avoid special chars in password

	// Find @ symbol to locate where credentials end
	atIndex := strings.LastIndex(dsn, "@")
	var searchStart int
	if atIndex >= 0 {
		// Start searching for ( after @
		searchStart = atIndex
	} else {
		// No credentials, search from beginning
		searchStart = 0
	}

	// Now find the ( and ) after the @ symbol
	n := len(dsn)
	i, j := -1, -1
	for k := searchStart; k < n; k++ {
		if dsn[k] == '(' {
			i = k
		}
		if dsn[k] == ')' && i >= 0 {
			// Only accept ) if we've already found (
			j = k
			break
		}
	}
	if i >= 0 && j > i {
		return dsn[i+1 : j], nil
	}
	return "", errors.New("invalid MySQL DSN")
}

func ParseClickHouse(dsn string) (addr string, err error) {
	// ClickHouse DSN formats:
	// tcp://host:port?database=dbname&username=user&password=pass
	// http://host:port?database=dbname
	// clickhouse://host:port/database?username=user&password=pass
	u, err := nurl.Parse(dsn)
	if err != nil {
		return "", err
	}

	// Return host with port
	if u.Port() != "" {
		return u.Host, nil
	}

	// Default ports based on scheme
	switch u.Scheme {
	case "tcp", "native":
		return u.Hostname() + ":9000", nil
	case "http":
		return u.Hostname() + ":8123", nil
	case "https":
		return u.Hostname() + ":8443", nil
	case "clickhouse":
		// Default to native port
		return u.Hostname() + ":9000", nil
	}

	return u.Host, nil
}

func ParseOracle(dsn string) (addr string, err error) {
	// Oracle DSN formats:
	// user/password@host:port/service_name
	// user/password@host:port/sid
	// user/password@//host:port/service_name
	// oracle://user:password@host:port/service_name

	// Try URL format first
	if strings.Contains(dsn, "://") {
		u, err := nurl.Parse(dsn)
		if err == nil && u.Host != "" {
			if u.Port() != "" {
				return u.Host, nil
			}
			return u.Hostname() + ":1521", nil // Oracle default port
		}
	}

	// Parse traditional Oracle format: user/password@host:port/service
	atIndex := strings.Index(dsn, "@")
	if atIndex < 0 {
		return "", errors.New("invalid Oracle DSN")
	}

	connStr := dsn[atIndex+1:]
	// Remove leading //
	connStr = strings.TrimPrefix(connStr, "//")

	// Extract host:port before /
	slashIndex := strings.Index(connStr, "/")
	var hostPort string
	if slashIndex > 0 {
		hostPort = connStr[:slashIndex]
	} else {
		hostPort = connStr
	}

	// If no port specified, add default
	if !strings.Contains(hostPort, ":") {
		hostPort = hostPort + ":1521"
	}

	return hostPort, nil
}

func ParseSQLServer(dsn string) (addr string, err error) {
	// SQL Server DSN formats:
	// sqlserver://username:password@host:port?database=dbname
	// server=host;port=1433;database=dbname;user id=user;password=pass
	// Server=host,port;Database=dbname;User Id=user;Password=pass

	// Try URL format first
	if strings.HasPrefix(dsn, "sqlserver://") || strings.HasPrefix(dsn, "mssql://") {
		u, err := nurl.Parse(dsn)
		if err != nil {
			return "", err
		}
		if u.Port() != "" {
			return u.Host, nil
		}
		return u.Hostname() + ":1433", nil // SQL Server default port
	}

	// Parse connection string format (key=value pairs)
	dsn = strings.ToLower(dsn)
	var host, port string

	// Split by semicolon
	pairs := strings.Split(dsn, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if strings.HasPrefix(pair, "server=") {
			serverVal := strings.TrimPrefix(pair, "server=")
			// Handle Server=host,port format
			if strings.Contains(serverVal, ",") {
				parts := strings.Split(serverVal, ",")
				host = parts[0]
				if len(parts) > 1 {
					port = parts[1]
				}
			} else {
				host = serverVal
			}
		} else if strings.HasPrefix(pair, "port=") {
			port = strings.TrimPrefix(pair, "port=")
		} else if strings.HasPrefix(pair, "host=") {
			host = strings.TrimPrefix(pair, "host=")
		}
	}

	if host == "" {
		return "", errors.New("invalid SQL Server DSN")
	}

	if port == "" {
		port = "1433" // SQL Server default port
	}

	return host + ":" + port, nil
}
