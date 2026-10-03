// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package dep is an application dependency that otelc instruments. Its rule
// adds an import of late, a package dep does not import, so compiling dep
// resolves that import with a nested build.
package dep

import "strings"

// Work is wrapped by the dep rule: the injected code calls into late, which
// only links because the nested build resolved its archive.
func Work() string {
	return strings.ToUpper("from dep")
}
