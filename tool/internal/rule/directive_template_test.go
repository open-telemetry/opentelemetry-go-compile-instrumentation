// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeTestData is a minimal template data value used to exercise
// DirectiveTemplate.Execute directly, without depending on the instrument
// package's funcTemplateData.
type executeTestData struct {
	Name string
}

func (d executeTestData) Fail() (string, error) {
	return "", errors.New("boom")
}

func TestDirectiveTemplate_Execute(t *testing.T) {
	tmpl, err := ParseDirectiveTemplate("hello {{.Name}}")
	require.NoError(t, err)

	result, err := tmpl.Execute(executeTestData{Name: "world"})

	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

// TestDirectiveTemplate_ExecuteError exercises the branch where the
// underlying text/template execution fails (here, a data method returning a
// non-nil error), verifying Execute wraps and surfaces that error.
func TestDirectiveTemplate_ExecuteError(t *testing.T) {
	tmpl, err := ParseDirectiveTemplate("{{.Fail}}")
	require.NoError(t, err)

	_, err = tmpl.Execute(executeTestData{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
