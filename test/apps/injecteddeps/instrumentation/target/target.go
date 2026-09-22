// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package target stands in for a package that an instrumentation module both
// ships and instruments, such as a vendor's own runtime support code.
package target

// Instrumented is flipped to true by the injected_target_flag rule. It stays
// false when that rule is not matched.
var Instrumented = false
