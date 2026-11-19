// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
)

const selectRows = "SELECT id FROM synthetic_rows"

type queryRows func(context.Context) (*sql.Rows, error)

func queryFor(ctx context.Context, database *sql.DB, endpoint string) (queryRows, func() error, error) {
	switch endpoint {
	case "db":
		return func(ctx context.Context) (*sql.Rows, error) {
			return database.QueryContext(ctx, selectRows)
		}, func() error { return nil }, nil
	case "conn":
		conn, err := database.Conn(ctx)
		if err != nil {
			return nil, nil, err
		}
		return func(ctx context.Context) (*sql.Rows, error) {
			return conn.QueryContext(ctx, selectRows)
		}, conn.Close, nil
	case "stmt":
		stmt, err := database.PrepareContext(ctx, selectRows)
		if err != nil {
			return nil, nil, err
		}
		return func(ctx context.Context) (*sql.Rows, error) {
			return stmt.QueryContext(ctx)
		}, stmt.Close, nil
	case "tx", "txstmt":
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		if endpoint == "tx" {
			return func(ctx context.Context) (*sql.Rows, error) {
				return tx.QueryContext(ctx, selectRows)
			}, tx.Rollback, nil
		}
		stmt, err := tx.PrepareContext(ctx, selectRows)
		if err != nil {
			return nil, nil, errors.Join(err, tx.Rollback())
		}
		return func(ctx context.Context) (*sql.Rows, error) {
			return stmt.QueryContext(ctx)
		}, func() error { return errors.Join(stmt.Close(), tx.Rollback()) }, nil
	default:
		return nil, nil, fmt.Errorf("unknown endpoint %q", endpoint)
	}
}

func consume(ctx context.Context, database *sql.DB, fixture *rowsDriver, endpoint string) (resultErr error) {
	if endpoint == "row" {
		var identifier int
		return database.QueryRowContext(ctx, selectRows).Scan(&identifier)
	}
	query, cleanup, err := queryFor(ctx, database, endpoint)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, cleanup()) }()
	rows, err := query(ctx)
	if err != nil {
		if fixture.mode == "query-error" {
			return nil
		}
		return err
	}
	if fixture.mode == "unclosed" {
		return nil
	}
	if fixture.mode == "scan-error" {
		if !rows.Next() {
			return errors.New("expected a row before conversion failure")
		}
		var timestamp time.Time
		scanErr := rows.Scan(&timestamp)
		closeErr := rows.Close()
		if scanErr == nil {
			return errors.New("expected a conversion error")
		}
		return closeErr
	}
	if fixture.mode == "cancel" {
		cancel := fixture.cancel
		go func() {
			<-fixture.reading
			cancel()
		}()
	}
	if fixture.mode == "race" {
		go fixture.cancel()
	}
	count := 0
	for {
		for rows.Next() {
			var identifier int
			if err := rows.Scan(&identifier); err != nil {
				return errors.Join(err, rows.Close())
			}
			count++
			if fixture.mode == "early" {
				return errors.Join(rows.Close(), rows.Close())
			}
			if fixture.mode == "delayed" {
				if count == 1 {
					_, between := otel.Tracer("dbrows-test").Start(context.Background(), "between-reads")
					between.End()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		if !rows.NextResultSet() {
			break
		}
	}
	iterationErr := rows.Err()
	closeErr := rows.Close()
	if fixture.mode == "next-error" || fixture.mode == "close-error" ||
		fixture.mode == "next-result-error" || fixture.mode == "cancel" {
		if iterationErr == nil && closeErr == nil {
			return errors.New("expected an iteration or close error")
		}
		return nil
	}
	if fixture.mode == "cancel-before-return" || fixture.mode == "race" {
		return nil
	}
	if iterationErr != nil || closeErr != nil {
		return errors.Join(iterationErr, closeErr)
	}
	want := 3
	if fixture.mode == "empty" {
		want = 0
	}
	if count != want {
		return fmt.Errorf("read %d rows, want %d", count, want)
	}
	return nil
}

func run(mode, endpoint string, repeat int) error {
	fixture := &rowsDriver{mode: mode, reading: make(chan struct{})}
	sql.Register("rows-test", fixture)
	database, err := sql.Open("rows-test", "synthetic")
	if err != nil {
		return err
	}
	defer database.Close()
	if mode == "no-parent" {
		return consume(context.Background(), database, fixture, endpoint)
	}
	ctx, parent := otel.Tracer("dbrows-test").Start(context.Background(), "operation")
	defer parent.End()
	if mode == "concurrent" {
		results := make(chan error, repeat)
		var queries sync.WaitGroup
		for range repeat {
			queries.Go(func() { results <- consume(ctx, database, fixture, endpoint) })
		}
		queries.Wait()
		close(results)
		var resultErr error
		for err := range results {
			resultErr = errors.Join(resultErr, err)
		}
		return resultErr
	}
	for range repeat {
		queryCtx, cancel := context.WithCancel(ctx)
		fixture.cancel = cancel
		if mode == "unclosed" {
			queryCtx = ctx
		}
		err := consume(queryCtx, database, fixture, endpoint)
		cancel()
		if err != nil {
			return err
		}
	}
	_, followup := otel.Tracer("dbrows-test").Start(ctx, "followup")
	followup.End()
	return nil
}

func main() {
	mode := flag.String("mode", "delayed", "rows lifecycle scenario")
	endpoint := flag.String("endpoint", "db", "query entry point")
	repeat := flag.Int("repeat", 1, "number of queries")
	wait := flag.Bool("wait", false, "wait for interrupt after queries finish")
	flag.Parse()
	if err := run(*mode, *endpoint, *repeat); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *wait {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		<-ctx.Done()
	}
}
