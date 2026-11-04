// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package driver

import "example.com/hookinit/registry"

func init() { registry.Register("early"); registry.Register("early-again") }
