// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main_test

// ProcessRequest is an external test function (package main_test). The rule
// uses has_package: main_test, whose declared name differs from the import-path
// tail "main". This is the discriminating case: target: main alone cannot
// distinguish this file from package main files in the same build; has_package
// is required. The non-matching counterpart is has-package-no-match, where the
// same import path is targeted with has_package: otherpkg and the rule is gated
// out because the declared name does not match.
func ProcessRequest(req string) error {
	println("processing:", req)
	return nil
}
