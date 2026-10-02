// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package extra is reachable from the hook only under the injectedtag build
// tag, so it enters the build plan only when that tag is set.
package extra

// Extra is flipped to true by the injected_extra_flag rule.
var Extra = false
