// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package generic

// Id stands in for a generic function of a third-party package.
func Id[T any](v T) T { return v }

// Convert has two type parameters, so an explicit instantiation of it lists
// both of them.
func Convert[T, U any](v T, f func(T) U) U { return f(v) }
