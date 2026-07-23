// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"encoding/json"
	"sync"
)

var (
	configMu     sync.RWMutex
	globalConfig map[string]any // Holds the "go" block configuration from .otel.yml
)

// RegisterConfig unmarshals the JSON-encoded configuration block injected at build time
// into the runtime configuration registry.
func RegisterConfig(jsonStr string) {
	if jsonStr == "" {
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		logger.Error("failed to parse injected otel configuration", "error", err)
		return
	}

	configMu.Lock()
	defer configMu.Unlock()
	
	// If globalConfig is nil, initialize it
	if globalConfig == nil {
		globalConfig = make(map[string]any)
	}
	// Merge parsed configuration
	for k, v := range parsed {
		globalConfig[k] = v
	}
}

// GetConfig returns the runtime configuration associated with the given
// instrumentation name, allowing hooks to read declarative configuration.
func GetConfig(instrumentationName string) map[string]any {
	configMu.RLock()
	defer configMu.RUnlock()

	if globalConfig == nil {
		return nil
	}

	// The configuration for an instrumentation is expected to be a map
	if val, ok := globalConfig[instrumentationName]; ok {
		if m, ok := val.(map[string]any); ok {
			return m
		}
	}
	return nil
}
