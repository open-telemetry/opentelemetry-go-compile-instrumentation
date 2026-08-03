// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

const unresolvedVersionSkipMsg = "skipping version-gated instrumentation because dependency version is unresolved"

// unresolvedVersionSkip reports whether a version-gated rule is being skipped
// because the dependency version could not be resolved from its compile path
// (common for replace/local/vendor sources that lack an @vX.Y.Z/ segment).
//
// Precondition: call only after the rule has already failed its version check
// (matchVersion / util.VersionInRange returned false) — this alone does not
// verify that the rule would otherwise have matched.
func unresolvedVersionSkip(depVersion, ruleVersion string) bool {
	return depVersion == "" && ruleVersion != ""
}

// warnUnresolvedVersionSkip emits a shared warning used by both setup matching
// and pin import selection when a version-gated rule/instrumentation is skipped
// because Dependency.Version is empty. Extra key/value attrs (e.g. "rule", name
// or "instrumentation", modPath) are appended after the common fields.
func warnUnresolvedVersionSkip(warn func(string, ...any), depImportPath, versionRange string, attrs ...any) {
	if warn == nil {
		return
	}
	args := append([]any{"dep", depImportPath, "version_range", versionRange}, attrs...)
	warn(unresolvedVersionSkipMsg, args...)
}
