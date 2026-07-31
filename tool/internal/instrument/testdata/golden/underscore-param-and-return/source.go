// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// Blank param + blank named return, the collision case from issue #736.
func UnderscoreParamReturnFunc(_ int) (_ error) {
	return nil
}

func main() {}
