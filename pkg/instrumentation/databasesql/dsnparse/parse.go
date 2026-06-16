// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dsnparse

import (
	nurl "net/url"
	"strings"
)

// DSNInfo holds the parsed server address components and database name from a
// data source name.
type DSNInfo struct {
	Host   string
	Port   string
	DBName string
}

// Addr returns the host:port pair. When Port is empty only the host is returned.
func (d DSNInfo) Addr() string {
	if d.Host == "" {
		return ""
	}
	if d.Port == "" {
		return d.Host
	}
	return d.Host + ":" + d.Port
}

// ParseDSN parses a driver-specific data source name and returns structured
// connection information. It tries multiple well-known formats in order and
// never panics. Unrecognised drivers return a zero-value DSNInfo.
func ParseDSN(driverName, dsn string) DSNInfo {
	switch driverName {
	case "postgres", "pgx", "postgresql":
		return parsePostgresDSN(dsn)
	case "mysql":
		return parseMySQLDSN(dsn)
	case "sqlite3", "sqlite":
		return parseSQLiteDSN(dsn)
	case "sqlserver", "mssql":
		return parseSQLServerDSN(dsn)
	case "clickhouse":
		return parseClickHouseDSN(dsn)
	case "godror", "oracle", "oci8", "go-oci8":
		return parseOracleDSN(dsn)
	}
	return DSNInfo{}
}

// ParseDbName extracts the database name from a generic DSN by finding the
// last '/' and trimming any query-string suffix. Retained for backward
// compatibility; prefer ParseDSN when the driver name is known.
func ParseDbName(dsn string) string {
	for i := len(dsn) - 1; i >= 0; i-- {
		if dsn[i] == '/' {
			dbname := dsn[i+1:]
			if idx := strings.IndexAny(dbname, "?&"); idx >= 0 {
				dbname = dbname[:idx]
			}
			if unesc, err := nurl.PathUnescape(dbname); err == nil {
				return unesc
			}
			return dbname
		}
	}
	return ""
}

// LegacyParseDSN wraps ParseDSN and preserves the original (addr, error) shape
// used by the db package's beforeOpenInstrumentation.
func LegacyParseDSN(driverName, dsn string) (string, error) {
	info := ParseDSN(driverName, dsn)
	if addr := info.Addr(); addr != "" {
		return addr, nil
	}
	return driverName, nil
}

// ---- PostgreSQL ---------------------------------------------------------------

// parsePostgresDSN handles both RFC 3986 URL format and the PostgreSQL libpq
// key=value connection string format.
//
// URL examples:
//
//	postgres://user:pass@host:5432/mydb
//	postgresql://host/mydb
//
// Key-value examples:
//
//	host=localhost port=5432 dbname=mydb
//	host='db.example.com' port=5432 dbname=prod user=alice
func parsePostgresDSN(dsn string) DSNInfo {
	if strings.Contains(dsn, "://") {
		if u, err := nurl.Parse(dsn); err == nil &&
			(u.Scheme == "postgres" || u.Scheme == "postgresql") {
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "5432"
			}
			dbName := strings.TrimPrefix(u.Path, "/")
			return DSNInfo{Host: host, Port: port, DBName: dbName}
		}
	}
	return parseLibpqKV(dsn)
}

// parseLibpqKV parses the PostgreSQL libpq keyword=value connection string.
// Values may be unquoted (terminated by whitespace) or single-quoted (with
// backslash escape support inside quotes).
func parseLibpqKV(dsn string) DSNInfo {
	info := DSNInfo{Host: "localhost", Port: "5432"}
	rest := strings.TrimSpace(dsn)
	for len(rest) > 0 {
		rest = strings.TrimLeft(rest, " \t\n\r")
		if rest == "" {
			break
		}
		eqIdx := strings.IndexByte(rest, '=')
		if eqIdx < 0 {
			break
		}
		key := strings.TrimSpace(rest[:eqIdx])
		rest = rest[eqIdx+1:]

		var val string
		if strings.HasPrefix(rest, "'") {
			rest = rest[1:]
			var b strings.Builder
			for len(rest) > 0 {
				switch {
				case rest[0] == '\\' && len(rest) > 1:
					b.WriteByte(rest[1])
					rest = rest[2:]
				case rest[0] == '\'':
					rest = rest[1:]
					val = b.String()
					goto nextKV
				default:
					b.WriteByte(rest[0])
					rest = rest[1:]
				}
			}
			val = b.String()
		} else {
			end := strings.IndexAny(rest, " \t\n\r")
			if end < 0 {
				end = len(rest)
			}
			val = rest[:end]
			rest = rest[end:]
		}
	nextKV:
		switch key {
		case "host":
			info.Host = val
		case "port":
			info.Port = val
		case "dbname":
			info.DBName = val
		}
	}
	return info
}

// ---- MySQL -------------------------------------------------------------------

// parseMySQLDSN handles the go-sql-driver/mysql DSN format:
//
//	[user[:password]@][protocol[(address)]]/dbname[?params]
//
// It also handles the non-standard form where the address is not wrapped in
// parentheses (e.g. user:pass@tcp:3306/dbname or user:pass@host:3306/dbname).
func parseMySQLDSN(dsn string) DSNInfo {
	// Find the last @ that precedes the first '(' so we skip '@' inside passwords.
	atIdx := -1
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '@' {
			atIdx = i
		}
		if dsn[i] == '(' {
			break
		}
	}
	rest := dsn
	if atIdx >= 0 {
		rest = dsn[atIdx+1:]
	}

	var addrStr, dbPart string

	if lp := strings.IndexByte(rest, '('); lp >= 0 {
		// Standard parenthesised address: proto(host:port)/dbname
		if rp := strings.IndexByte(rest[lp:], ')'); rp >= 0 {
			addrStr = rest[lp+1 : lp+rp]
			dbPart = rest[lp+rp+1:]
		}
	} else {
		// Non-standard: no parentheses around the address.
		// Locate the '/' that separates the address from the database name.
		if sl := strings.IndexByte(rest, '/'); sl >= 0 {
			addrStr = rest[:sl]
			dbPart = rest[sl:]
		} else {
			addrStr = rest
		}
		addrStr = mysqlStripProtocol(addrStr)
	}

	host, port := mysqlSplitAddr(addrStr)
	dbName := mysqlDBName(dbPart)
	return DSNInfo{Host: host, Port: port, DBName: dbName}
}

// mysqlKnownProtocols lists the transport keywords used by go-sql-driver/mysql.
var mysqlKnownProtocols = map[string]bool{
	"tcp": true, "unix": true, "udp": true, "pipe": true,
}

// mysqlStripProtocol removes a leading "proto:" prefix when proto is a known
// MySQL transport keyword, normalising a bare port number to "localhost:port".
func mysqlStripProtocol(addr string) string {
	colon := strings.IndexByte(addr, ':')
	if colon < 0 {
		return addr
	}
	proto := addr[:colon]
	if !mysqlKnownProtocols[proto] {
		return addr
	}
	rest := addr[colon+1:]
	if rest == "" {
		return ""
	}
	// If rest is pure digits it is just a port number with an implicit localhost.
	allDigits := true
	for _, c := range rest {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return "localhost:" + rest
	}
	return rest
}

// mysqlSplitAddr splits a "host:port" string, defaulting to port 3306 when no
// port is present. IPv6 addresses enclosed in brackets are handled correctly.
func mysqlSplitAddr(addr string) (host, port string) {
	if addr == "" {
		return "", ""
	}
	if strings.HasPrefix(addr, "[") {
		if rb := strings.LastIndexByte(addr, ']'); rb >= 0 {
			host = addr[1:rb]
			rest := addr[rb+1:]
			if strings.HasPrefix(rest, ":") {
				return host, rest[1:]
			}
			return host, "3306"
		}
	}
	if colon := strings.LastIndexByte(addr, ':'); colon >= 0 {
		return addr[:colon], addr[colon+1:]
	}
	// Unix socket paths contain '/' but no port.
	if strings.ContainsRune(addr, '/') {
		return addr, ""
	}
	return addr, "3306"
}

// mysqlDBName trims the leading '/' and any query string from a dbname segment.
func mysqlDBName(s string) string {
	s = strings.TrimPrefix(s, "/")
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	return s
}

// ---- SQLite ------------------------------------------------------------------

// parseSQLiteDSN handles sqlite3 / sqlite DSN strings. SQLite is always
// file-local, so Host and Port are not populated. DBName is set to the
// filename (or ":memory:" for in-memory databases).
func parseSQLiteDSN(dsn string) DSNInfo {
	return DSNInfo{Host: "sqlite3", DBName: sqliteDBName(dsn)}
}

// sqliteDBName extracts the database name from a SQLite DSN. It handles
// file: URI schemes, the :memory: shorthand, and bare filenames.
func sqliteDBName(dsn string) string {
	if strings.HasPrefix(dsn, "file:") {
		path := dsn[len("file:"):]
		if i := strings.IndexByte(path, '?'); i >= 0 {
			path = path[:i]
		}
		bare := strings.TrimPrefix(path, "//")
		if bare == ":memory:" {
			return ":memory:"
		}
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			return path[i+1:]
		}
		return path
	}
	if dsn == ":memory:" {
		return ":memory:"
	}
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		dsn = dsn[:i]
	}
	if i := strings.LastIndexByte(dsn, '/'); i >= 0 {
		return dsn[i+1:]
	}
	return dsn
}

// ---- SQL Server --------------------------------------------------------------

// parseSQLServerDSN handles SQL Server DSNs in both URL format and the
// ADO.NET semicolon-delimited key=value connection-string format.
//
// URL examples:
//
//	sqlserver://user:pass@host:1433?database=mydb
//	mssql://host:1433?database=mydb
//
// ADO.NET examples:
//
//	server=host;port=1433;database=mydb;user id=sa;password=secret
//	Server=host,1433;Database=mydb;User Id=sa;Password=secret
func parseSQLServerDSN(dsn string) DSNInfo {
	if strings.Contains(dsn, "://") {
		if u, err := nurl.Parse(dsn); err == nil {
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "1433"
			}
			dbName := u.Query().Get("database")
			return DSNInfo{Host: host, Port: port, DBName: dbName}
		}
	}
	return parseSQLServerKV(dsn)
}

// parseSQLServerKV parses the ADO.NET semicolon-delimited key=value format.
func parseSQLServerKV(dsn string) DSNInfo {
	var host, port, dbName string
	for _, pair := range strings.Split(dsn, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, val, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "server", "data source":
			// Accept both "host,port" and "host\instance" forms.
			if comma := strings.IndexByte(val, ','); comma >= 0 {
				host = val[:comma]
				port = val[comma+1:]
			} else if bs := strings.IndexByte(val, '\\'); bs >= 0 {
				host = val[:bs]
			} else {
				host = val
			}
		case "port":
			port = val
		case "database", "initial catalog":
			dbName = val
		}
	}
	if host != "" && port == "" {
		port = "1433"
	}
	return DSNInfo{Host: host, Port: port, DBName: dbName}
}

// ---- ClickHouse --------------------------------------------------------------

// parseClickHouseDSN handles ClickHouse DSNs, which are always URL-formatted.
func parseClickHouseDSN(dsn string) DSNInfo {
	u, err := nurl.Parse(dsn)
	if err != nil {
		return DSNInfo{}
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "http":
			port = "8123"
		case "https":
			port = "8443"
		default: // tcp, native, clickhouse
			port = "9000"
		}
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		dbName = u.Query().Get("database")
	}
	return DSNInfo{Host: host, Port: port, DBName: dbName}
}

// ---- Oracle ------------------------------------------------------------------

// parseOracleDSN handles Oracle DSNs in URL format or the traditional
// user/pass@host:port/service notation.
func parseOracleDSN(dsn string) DSNInfo {
	if strings.Contains(dsn, "://") {
		if u, err := nurl.Parse(dsn); err == nil && u.Host != "" {
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "1521"
			}
			dbName := strings.TrimPrefix(u.Path, "/")
			return DSNInfo{Host: host, Port: port, DBName: dbName}
		}
	}
	atIdx := strings.IndexByte(dsn, '@')
	if atIdx < 0 {
		return DSNInfo{}
	}
	connStr := strings.TrimPrefix(dsn[atIdx+1:], "//")
	var hostPort, dbName string
	if sl := strings.IndexByte(connStr, '/'); sl >= 0 {
		hostPort = connStr[:sl]
		dbName = connStr[sl+1:]
	} else {
		hostPort = connStr
	}
	host, port := oracleSplitHostPort(hostPort)
	return DSNInfo{Host: host, Port: port, DBName: dbName}
}

func oracleSplitHostPort(hp string) (host, port string) {
	if i := strings.LastIndexByte(hp, ':'); i >= 0 {
		return hp[:i], hp[i+1:]
	}
	return hp, "1521"
}
