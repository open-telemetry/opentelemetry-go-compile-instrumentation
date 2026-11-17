// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"encoding/json"
	"sync"
)

var embeddedConfiguration struct {
	sync.RWMutex
	values map[string]any
}

// RegisterConfiguration installs the configuration resolved by otelc during
// setup. Multiple generated package files may register the same snapshot, so
// identical registrations are harmless and the first registration wins.
func RegisterConfiguration(snapshot string) {
	var values map[string]any
	if err := json.Unmarshal([]byte(snapshot), &values); err != nil {
		return
	}
	embeddedConfiguration.Lock()
	if embeddedConfiguration.values == nil {
		embeddedConfiguration.values = values
	}
	embeddedConfiguration.Unlock()
}

// ConfigurationValue returns one resolved application configuration value.
// Values come from the build-time configuration snapshot; runtime environment
// overrides are intentionally handled separately by instrumentation consumers.
func ConfigurationValue(path string) (any, bool) {
	embeddedConfiguration.RLock()
	value, ok := embeddedConfiguration.values[path]
	embeddedConfiguration.RUnlock()
	return value, ok
}
