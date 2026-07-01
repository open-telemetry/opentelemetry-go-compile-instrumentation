// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/database/sql/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/database/sql"
	instrumentationKey  = "DATABASE"
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

// dbClientEnabler controls whether client instrumentation is enabled
type dbClientEnabler struct{}

func (n dbClientEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var clientEnabler = dbClientEnabler{}

func beforeOpenInstrumentation(ictx hook.HookContext, driverName, dataSourceName string) {
	info := ParseDSN(driverName, dataSourceName)
	addr := info.Addr()
	if addr == "" {
		addr = "unknown"
	}
	dbName := info.DBName
	if dbName == "" {
		dbName = ParseDbName(dataSourceName)
	}
	ictx.SetData(map[string]string{
		"endpoint": addr,
		"driver":   driverName,
		"dsn":      dataSourceName,
		"dbName":   dbName,
	})
}

func afterOpenInstrumentation(ictx hook.HookContext, db *sql.DB, err error) {
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

func beforePingContextInstrumentation(ictx hook.HookContext, db *sql.DB, ctx context.Context) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "ping", "ping", db.Endpoint, db.DriverName, db.DSN, db.DbName)
}

func afterPingContextInstrumentation(ictx hook.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforePrepareContextInstrumentation(ictx hook.HookContext, db *sql.DB, ctx context.Context, query string) {
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

func afterPrepareContextInstrumentation(ictx hook.HookContext, stmt *sql.Stmt, err error) {
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

func beforeExecContextInstrumentation(
	ictx hook.HookContext,
	db *sql.DB,
	ctx context.Context,
	query string,
	args ...interface{},
) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "exec", query, db.Endpoint, db.DriverName, db.DSN, db.DbName, args...)
}

func afterExecContextInstrumentation(ictx hook.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeQueryContextInstrumentation(
	ictx hook.HookContext,
	db *sql.DB,
	ctx context.Context,
	query string,
	args ...interface{},
) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "query", query, db.Endpoint, db.DriverName, db.DSN, db.DbName, args...)
}

func afterQueryContextInstrumentation(ictx hook.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxInstrumentation(ictx hook.HookContext, db *sql.DB, ctx context.Context, opts *sql.TxOptions) {
	if !clientEnabler.Enable() {
		return
	}
	if db == nil {
		return
	}
	instrumentStart(ictx, ctx, "begin", "START TRANSACTION", db.Endpoint, db.DriverName, db.DSN, db.DbName)
}

func afterTxInstrumentation(ictx hook.HookContext, tx *sql.Tx, err error) {
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

func beforeConnInstrumentation(ictx hook.HookContext, db *sql.DB, ctx context.Context) {
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

func afterConnInstrumentation(ictx hook.HookContext, conn *sql.Conn, err error) {
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

func beforeConnPingContextInstrumentation(ictx hook.HookContext, conn *sql.Conn, ctx context.Context) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "ping", "ping", conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName)
}

func afterConnPingContextInstrumentation(ictx hook.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeConnPrepareContextInstrumentation(ictx hook.HookContext, conn *sql.Conn, ctx context.Context, query string) {
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

func afterConnPrepareContextInstrumentation(ictx hook.HookContext, stmt *sql.Stmt, err error) {
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

func beforeConnExecContextInstrumentation(
	ictx hook.HookContext,
	conn *sql.Conn,
	ctx context.Context,
	query string,
	args ...interface{},
) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "exec", query, conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName, args...)
}

func afterConnExecContextInstrumentation(ictx hook.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeConnQueryContextInstrumentation(
	ictx hook.HookContext,
	conn *sql.Conn,
	ctx context.Context,
	query string,
	args ...interface{},
) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "query", query, conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName, args...)
}

func afterConnQueryContextInstrumentation(ictx hook.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeConnTxInstrumentation(ictx hook.HookContext, conn *sql.Conn, ctx context.Context, opts *sql.TxOptions) {
	if !clientEnabler.Enable() {
		return
	}
	if conn == nil {
		return
	}
	instrumentStart(ictx, ctx, "start", "START TRANSACTION", conn.Endpoint, conn.DriverName, conn.DSN, conn.DbName)
}

func afterConnTxInstrumentation(ictx hook.HookContext, tx *sql.Tx, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxPrepareContextInstrumentation(ictx hook.HookContext, tx *sql.Tx, ctx context.Context, query string) {
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

func afterTxPrepareContextInstrumentation(ictx hook.HookContext, stmt *sql.Stmt, err error) {
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

func beforeTxStmtContextInstrumentation(ictx hook.HookContext, tx *sql.Tx, ctx context.Context, stmt *sql.Stmt) {
	if !clientEnabler.Enable() {
		return
	}
	if stmt == nil || stmt.Data == nil {
		return
	}
	ictx.SetData(map[string]string{
		"endpoint": stmt.Data["endpoint"],
		"driver":   stmt.Data["driver"],
		"dsn":      stmt.DSN,
		"sql":      stmt.Data["sql"],
		"dbName":   stmt.Data["dbName"],
	})
}

func afterTxStmtContextInstrumentation(ictx hook.HookContext, stmt *sql.Stmt) {
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

func beforeTxExecContextInstrumentation(
	ictx hook.HookContext,
	tx *sql.Tx,
	ctx context.Context,
	query string,
	args ...interface{},
) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, ctx, "exec", query, tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName, args...)
}

func afterTxExecContextInstrumentation(ictx hook.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxQueryContextInstrumentation(
	ictx hook.HookContext,
	tx *sql.Tx,
	ctx context.Context,
	query string,
	args ...interface{},
) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, ctx, "query", query, tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName, args...)
}

func afterTxQueryContextInstrumentation(ictx hook.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxCommitInstrumentation(ictx hook.HookContext, tx *sql.Tx) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, context.Background(), "commit", "COMMIT", tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName)
}

func afterTxCommitInstrumentation(ictx hook.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeTxRollbackInstrumentation(ictx hook.HookContext, tx *sql.Tx) {
	if !clientEnabler.Enable() {
		return
	}
	if tx == nil {
		return
	}
	instrumentStart(ictx, context.Background(), "rollback", "ROLLBACK", tx.Endpoint, tx.DriverName, tx.DSN, tx.DbName)
}

func afterTxRollbackInstrumentation(ictx hook.HookContext, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeStmtExecContextInstrumentation(
	ictx hook.HookContext,
	stmt *sql.Stmt,
	ctx context.Context,
	args ...interface{},
) {
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

func afterStmtExecContextInstrumentation(ictx hook.HookContext, result sql.Result, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func beforeStmtQueryContextInstrumentation(
	ictx hook.HookContext,
	stmt *sql.Stmt,
	ctx context.Context,
	args ...interface{},
) {
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

func afterStmtQueryContextInstrumentation(ictx hook.HookContext, rows *sql.Rows, err error) {
	if !clientEnabler.Enable() {
		return
	}
	instrumentEnd(ictx, err)
}

func instrumentStart(
	ictx hook.HookContext,
	ctx context.Context,
	spanName, query, endpoint, driverName, dsn, dbName string,
	args ...interface{},
) {
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

func instrumentEnd(ictx hook.HookContext, err error) {
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
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		logger.Info("DB client instrumentation initialized")
	})
}
