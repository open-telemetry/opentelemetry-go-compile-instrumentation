// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"net/http"
	"time"
)

// Context stands in for a web framework's request context, such as
// *gin.Context, which implements context.Context with pointer methods.
type Context struct {
	Request *http.Request
}

func (*Context) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*Context) Done() <-chan struct{}       { return nil }
func (*Context) Err() error                  { return nil }
func (*Context) Value(any) any               { return nil }
