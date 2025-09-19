// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

type InstRule interface {
	GetName() string // The name of the rule
	GetPath() string // The path of the rule
}
