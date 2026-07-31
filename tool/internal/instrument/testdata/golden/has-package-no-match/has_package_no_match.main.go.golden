// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// ProcessRequest is targeted by a rule scoped to "otherpkg" (has_package:
// otherpkg). This fixture declares package main, so the declared package clause
// does not match and the rule is gated out. The golden output stays
// byte-identical to this source. The matching counterpart is has-package-match.
func ProcessRequest(req string) error {
	println("processing:", req)
	return nil
}
