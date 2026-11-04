// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "example.com/hookinit/registry"

func init() { registry.Register("last-init"); after.names = nil }
