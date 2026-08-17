// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsTestBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []string
		want    bool
	}{
		{name: "production sources", sources: []string{"handler.go"}, want: false},
		{name: "in-package test file", sources: []string{"handler.go", "handler_test.go"}, want: true},
		{name: "generated testmain", sources: []string{"_testmain.go"}, want: true},
		{name: "empty", sources: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isTestBuild(tt.sources))
		})
	}
}
