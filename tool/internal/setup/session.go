// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"

	"go.opentelemetry.io/otelc/tool/util"
)

// cleanupOwnedSession removes the session directory of a build that owns its
// session: one that inherits none from an outer otelc build. An inherited
// session belongs to the outer build and is never cleaned here.
func cleanupOwnedSession() error {
	if os.Getenv(util.EnvOtelcSessionDir) != "" {
		return nil
	}
	return os.RemoveAll(util.GetBuildTemp(util.SessionTempDir))
}

// sessionEnv adds the session directory to the env of the go command an
// owning build runs, so its toolexec processes and any otelc build its
// binaries run inherit the session.
func sessionEnv(env []string) []string {
	if os.Getenv(util.EnvOtelcSessionDir) != "" {
		return env
	}
	return append(env, util.EnvOtelcSessionDir+"="+util.GetBuildTemp(util.SessionTempDir))
}
