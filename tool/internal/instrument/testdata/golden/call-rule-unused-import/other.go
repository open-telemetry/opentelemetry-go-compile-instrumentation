// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-unused-import/helpers/lib"

type traced struct{ c *lib.Client }

func (t traced) IsTraced() bool { return t.c.Traced }

func tracedOf(c *lib.Client) traced { return traced{c} }

// This file uses lib elsewhere, so its import is kept.
func otherClient() *lib.Client { return lib.New("b") }
