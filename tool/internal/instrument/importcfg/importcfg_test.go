// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package importcfg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	input := `# comment
packagefile fmt=/path/to/fmt.a
packagefile net/http=/path/to/net/http.a
importmap example.com/pkg=example.com/pkg/v2
packagefile example.com/pkg/v2=/path/to/pkg.a
modinfo "abc123"
`

	cfg, err := parse(bytes.NewReader([]byte(input)))
	require.NoError(t, err)

	assert.Equal(t, "/path/to/fmt.a", cfg.PackageFile["fmt"])
	assert.Equal(t, "/path/to/net/http.a", cfg.PackageFile["net/http"])
	assert.Equal(t, "/path/to/pkg.a", cfg.PackageFile["example.com/pkg/v2"])
	assert.Equal(t, "example.com/pkg/v2", cfg.ImportMap["example.com/pkg"])
	assert.Equal(t, []string{`modinfo "abc123"`}, cfg.Extras)
}

func TestParseFile(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "importcfg")

	content := `packagefile fmt=/path/to/fmt.a
packagefile strings=/path/to/strings.a
`
	err := os.WriteFile(filename, []byte(content), 0644)
	require.NoError(t, err)

	cfg, err := ParseFile(filename)
	require.NoError(t, err)

	assert.Equal(t, "/path/to/fmt.a", cfg.PackageFile["fmt"])
	assert.Equal(t, "/path/to/strings.a", cfg.PackageFile["strings"])
}

func TestWrite(t *testing.T) {
	cfg := ImportConfig{
		PackageFile: map[string]string{
			"fmt":      "/path/to/fmt.a",
			"net/http": "/path/to/net/http.a",
		},
		ImportMap: map[string]string{
			"example.com/pkg": "example.com/pkg/v2",
		},
		Extras: []string{`modinfo "abc123"`},
	}

	var buf bytes.Buffer
	err := cfg.write(&buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "packagefile fmt=/path/to/fmt.a\n")
	assert.Contains(t, output, "packagefile net/http=/path/to/net/http.a\n")
	assert.Contains(t, output, "importmap example.com/pkg=example.com/pkg/v2\n")
	assert.Contains(t, output, `modinfo "abc123"`)
}

func TestWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "importcfg")

	cfg := ImportConfig{
		PackageFile: map[string]string{
			"fmt": "/path/to/fmt.a",
		},
	}

	err := cfg.WriteFile(filename)
	require.NoError(t, err)

	content, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
}

func TestRoundTrip(t *testing.T) {
	input := `# comment line
packagefile fmt=/usr/local/go/pkg/linux_amd64/fmt.a
packagefile net/http=/usr/local/go/pkg/linux_amd64/net/http.a
importmap example.com/old=example.com/new
packagefile example.com/new=/tmp/new.a
modinfo "version-info"
`

	// Parse
	cfg, err := parse(bytes.NewReader([]byte(input)))
	require.NoError(t, err)

	// Write
	var buf bytes.Buffer
	err = cfg.write(&buf)
	require.NoError(t, err)

	// Parse again
	cfg2, err := parse(&buf)
	require.NoError(t, err)

	// Verify round-trip
	assert.Equal(t, cfg.PackageFile, cfg2.PackageFile)
	assert.Equal(t, cfg.ImportMap, cfg2.ImportMap)
	assert.Equal(t, cfg.Extras, cfg2.Extras)
}
