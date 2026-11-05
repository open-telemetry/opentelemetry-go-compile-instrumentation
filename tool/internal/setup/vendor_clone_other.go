// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package setup

import "errors"

// platformCloneFile is not supported on this operating system. linkOrCopyFile
// then uses a hard link or a copy.
func platformCloneFile(string, string) error {
	return errors.ErrUnsupported
}
