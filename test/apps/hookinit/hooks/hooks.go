// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"example.com/hookinit/registry"
	"go.opentelemetry.io/otelc/pkg/hook"
)

type recorder struct{ names []string }

var before *recorder
var after = &recorder{}

func init()                                  { before = &recorder{}; registry.Register("own-init") }
func Before(_ hook.HookContext, name string) { before.names = append(before.names, name) }
func After(ctx hook.HookContext)             { after.names = append(after.names, ctx.GetParam(0).(string)) }
func Seen() ([]string, []string)             { return before.names, after.names }
