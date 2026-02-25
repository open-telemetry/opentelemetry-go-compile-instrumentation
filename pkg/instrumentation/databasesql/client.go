// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"database/sql"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/databasesql/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/databasesql"
	instrumentationKey  = "DATABASE"
)

var (
	logger   = shared.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

// dbClientEnabler controls whether client instrumentation is enabled
type dbClientEnabler struct{}

func (n dbClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var clientEnabler = dbClientEnabler{}

func beforeOpenInstrumentation(ictx inst.HookContext, driverName, dataSourceName string) {
	addr, err := parseDSN(driverName, dataSourceName)
	if err != nil {
		addr = dataSourceName
	}
	dbName := ParseDbName(dataSourceName)
	ictx.SetData(map[string]string{
		"endpoint": addr,
		"driver":   driverName,
		"dsn":      dataSourceName,
		"dbName":   dbName,
	})
}

func afterOpenInstrumentation(ictx inst.HookContext, db *sql.DB, err error) {
	if db == nil || ictx.GetData() == nil {
		return
	}
	data, ok := ictx.GetData().(map[string]string)
	if !ok {
		return
	}
	endpoint, ok := data["endpoint"]
	if ok {
		db.Endpoint = endpoint
	}
	driver, ok := data["driver"]
	if ok {
		db.DriverName = driver
	}
	dsn, ok := data["dsn"]
	if ok {
		db.DSN = dsn
	}
	dbName, ok := data["dbName"]
	if ok {
		db.DbName = dbName
	}
}

func beforePingContextInstrumentation(ictx inst.HookContext, db *sql.DB, ctx context.Context) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "ping", "ping", db.Endpoint, db.DriverName, db.DSN, db.DbName)
}

func afterPingContextInstrumentation(ictx inst.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforePrepareContextInstrumentation(ictx inst.HookContext, db *sql.DB, ctx context.Context, query string) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	ictx.SetData(map[string]string{
		"endpoint": db.Endpoint,
		"sql":      query,
		"driver":   db.DriverName,
		"dsn":      db.DSN,
		"dbName":   db.DbName,
	})
}

func afterPrepareContextInstrumentation(ictx inst.HookContext, stmt *sql.Stmt, err error) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil {
		return
	}
	callDataMap, ok := ictx.GetData().(map[string]string)
	if !ok {
		return
	}
	stmt.Data = map[string]string{
		"endpoint": callDataMap["endpoint"],
		"sql":      callDataMap["sql"],
		"driver":   callDataMap["driver"],
		"dbName":   callDataMap["dbName"],
	}
	stmt.DSN = callDataMap["dsn"]
}

func beforeExecContextInstrumentation(ictx inst.HookContext, db *sql.DB, ctx context.Context, query string, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "exec", query, db.Endpoint, db.DriverName, db.DSN, db.DbName, args...)
}

func afterExecContextInstrumentation(ictx inst.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeQueryContextInstrumentation(ictx inst.HookContext, db *sql.DB, ctx context.Context, query string, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "query", query, db.Endpoint, db.DriverName, db.DSN, db.DbName, args...)
}

func afterQueryContextInstrumentation(ictx inst.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxInstrumentation(ictx inst.HookContext, db *sql.DB, ctx context.Context, opts *sql.TxOptions) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "begin", "START TRANSACTION", db.Endpoint, db.DriverName, db.DSN, db.DbName)
}

func afterTxInstrumentation(ictx inst.HookContext, tx *sql.Tx, err error) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil || ictx.GetData() == nil {
		return
	}
	callData, ok := ictx.GetData().(map[string]interface{})
	if !ok {
		return
	}
	dbRequest, ok := callData["req"].(semconv.DatabaseSqlRequest)
	if !ok {
		return
	}
	tx.Endpoint = dbRequest.Endpoint
	tx.DriverName = dbRequest.DriverName
	tx.DSN = dbRequest.Dsn
	tx.DbName = dbRequest.DbName
	instrumentEnd(ictx, err)
}

func beforeConnInstrumentation(ictx inst.HookContext, db *sql.DB, ctx context.Context) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	ictx.SetData(map[string]string{
		"endpoint": db.Endpoint,
		"driver":   db.DriverName,
		"dsn":      db.DSN,
		"dbName":   db.DbName,
	})
}

func afterConnInstrumentation(ictx inst.HookContext, conn *sql.Conn, err error) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	data, ok := ictx.GetData().(map[string]string)
	if !ok {
		return
	}
	endpoint, ok := data["endpoint"]
	if ok {
		conn.Endpoint = endpoint
	}
	driverName, ok := data["driver"]
	if ok {
		conn.DriverName = driverName
	}
	dsn, ok := data["dsn"]
	if ok {
		conn.DSN = dsn
	}
	dbName, ok := data["dbName"]
	if ok {
		conn.DbName = dbName
	}
}

func beforeConnPingContextInstrumentation(ictx inst.HookContext, conn *sql.Conn, ctx context.Context) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "ping", "ping", conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName)
}

func afterConnPingContextInstrumentation(ictx inst.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeConnPrepareContextInstrumentation(ictx inst.HookContext, conn *sql.Conn, ctx context.Context, query string) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	ictx.SetData(map[string]string{
		"endpoint": conn.Endpoint,
		"sql":      query,
		"driver":   conn.DriverName,
		"dsn":      conn.DSN,
		"dbName":   conn.DbName,
	})
}

func afterConnPrepareContextInstrumentation(ictx inst.HookContext, stmt *sql.Stmt, err error) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil {
		return
	}
	callDataMap, ok := ictx.GetData().(map[string]string)
	if !ok {
		return
	}
	stmt.Data = map[string]string{
		"endpoint": callDataMap["endpoint"],
		"sql":      callDataMap["sql"],
		"driver":   callDataMap["driver"],
		"dbName":   callDataMap["dbName"],
	}
	stmt.DSN = callDataMap["dsn"]
}

func beforeConnExecContextInstrumentation(ictx inst.HookContext, conn *sql.Conn, ctx context.Context, query string, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "exec", query, conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName, args...)
}

func afterConnExecContextInstrumentation(ictx inst.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeConnQueryContextInstrumentation(ictx inst.HookContext, conn *sql.Conn, ctx context.Context, query string, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "query", query, conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName, args...)
}

func afterConnQueryContextInstrumentation(ictx inst.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeConnTxInstrumentation(ictx inst.HookContext, conn *sql.Conn, ctx context.Context, opts *sql.TxOptions) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "start", "START TRANSACTION", conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName)
}

func afterConnTxInstrumentation(ictx inst.HookContext, tx *sql.Tx, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxPrepareContextInstrumentation(ictx inst.HookContext, tx *sql.Tx, ctx context.Context, query string) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	ictx.SetData(map[string]string{
		"endpoint": tx.Endpoint,
		"sql":      query,
		"driver":   tx.DriverName,
		"dsn":      tx.DSN,
		"dbName":   tx.DbName,
	})
}

func afterTxPrepareContextInstrumentation(ictx inst.HookContext, stmt *sql.Stmt, err error) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil {
		return
	}
	callDataMap, ok := ictx.GetData().(map[string]string)
	if !ok {
		return
	}
	stmt.Data = map[string]string{
		"endpoint": callDataMap["endpoint"],
		"sql":      callDataMap["sql"],
		"driver":   callDataMap["driver"],
		"dbName":   callDataMap["dbName"],
	}
	stmt.DSN = callDataMap["dsn"]
}

func beforeTxStmtContextInstrumentation(ictx inst.HookContext, tx *sql.Tx, ctx context.Context, stmt *sql.Stmt) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil {
		return
	}
	ictx.SetData(map[string]string{
		"endpoint": stmt.Data["endpoint"],
		"driver":   stmt.Data["driver"],
		"dsn":      stmt.DSN,
		"dbName":   stmt.Data["dbName"],
	})
}

func afterTxStmtContextInstrumentation(ictx inst.HookContext, stmt *sql.Stmt) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil {
		return
	}
	data, ok := ictx.GetData().(map[string]string)
	if !ok {
		return
	}
	stmt.Data = map[string]string{}
	endpoint, ok := data["endpoint"]
	if ok {
		stmt.Data["endpoint"] = endpoint
	}
	driverName, ok := data["driver"]
	if ok {
		stmt.Data["driver"] = driverName
	}
	dsn, ok := data["dsn"]
	if ok {
		stmt.Data["dsn"] = dsn
	}
	dbName, ok := data["dbName"]
	if ok {
		stmt.Data["dbName"] = dbName
	}
}

func beforeTxExecContextInstrumentation(ictx inst.HookContext, tx *sql.Tx, ctx context.Context, query string, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, ctx, "exec", query, tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName, args...)
}

func afterTxExecContextInstrumentation(ictx inst.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxQueryContextInstrumentation(ictx inst.HookContext, tx *sql.Tx, ctx context.Context, query string, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, ctx, "query", query, tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName, args...)
}

func afterTxQueryContextInstrumentation(ictx inst.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxCommitInstrumentation(ictx inst.HookContext, tx *sql.Tx) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, context.Background(), "commit", "COMMIT", tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName)
}

func afterTxCommitInstrumentation(ictx inst.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxRollbackInstrumentation(ictx inst.HookContext, tx *sql.Tx) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, context.Background(), "rollback", "ROLLBACK", tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName)
}

func afterTxRollbackInstrumentation(ictx inst.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeStmtExecContextInstrumentation(ictx inst.HookContext, stmt *sql.Stmt, ctx context.Context, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil {
		return
	}
	sql1, endpoint, driverName, dsn, dbName := "", "", "", "", ""
	if stmt.Data != nil {
		sql1, endpoint, driverName, dsn, dbName = stmt.Data["sql"], stmt.Data["endpoint"], stmt.Data["driver"], stmt.DSN, stmt.Data["dbName"]
	}
	instrumentStart(ictx, ctx, "exec", sql1, endpoint, driverName, dsn, dbName, args...)
}

func afterStmtExecContextInstrumentation(ictx inst.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeStmtQueryContextInstrumentation(ictx inst.HookContext, stmt *sql.Stmt, ctx context.Context, args ...any) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil {
		return
	}
	sql1, endpoint, driverName, dsn, dbName := "", "", "", "", ""
	if stmt.Data != nil {
		sql1, endpoint, driverName, dsn, dbName = stmt.Data["sql"], stmt.Data["endpoint"], stmt.Data["driver"], stmt.DSN, stmt.Data["dbName"]
	}
	instrumentStart(ictx, ctx, "query", sql1, endpoint, driverName, dsn, dbName, args...)
}

func afterStmtQueryContextInstrumentation(ictx inst.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func instrumentStart(ictx inst.HookContext, ctx context.Context, spanName, query, endpoint, driverName, dsn, dbName string, args ...any) {
	if !clientEnabler.Enable() {
		logger.Debug("Db client instrumentation disabled")
		return
	}
	initInstrumentation()
	req := semconv.DatabaseSqlRequest{
		OpType:     calOp(query),
		Sql:        query,
		Endpoint:   endpoint,
		DriverName: driverName,
		Dsn:        dsn,
		Params:     args,
		DbName:     dbName,
	}
	// Get trace attributes from semconv
	attrs := semconv.DbClientRequestTraceAttrs(req)

	// Start span
	ctx, span := tracer.Start(ctx,
		req.OpType,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	// Store data for after hook
	ictx.SetData(map[string]interface{}{
		"ctx":   ctx,
		"span":  span,
		"req":   req,
		"start": time.Now(),
	})
}

func instrumentEnd(ictx inst.HookContext, err error) {
	if !clientEnabler.Enable() {
		logger.Debug("Db client instrumentation disabled")
		return
	}
	if ictx.GetData() == nil {
		return
	}
	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		logger.Debug("instrumentEnd: no span from before hook")
		return
	}
	defer span.End()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
}

func calOp(sql string) string {
	sqls := strings.Split(sql, " ")
	var op string
	if len(sqls) > 0 {
		op = sqls[0]
	}
	return op
}

// moduleVersion extracts the version from the Go module system.
// Falls back to "dev" if version cannot be determined.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Return the main module version
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/databasesql", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("DB client instrumentation initialized")
	})
}
