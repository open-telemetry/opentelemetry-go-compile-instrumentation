// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package registry

var Names []string

func Register(name string) { Names = append(Names, name) }
