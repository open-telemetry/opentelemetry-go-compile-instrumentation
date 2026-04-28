// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package imports

import (
	"bytes"
	"errors"
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
	err := os.WriteFile(filename, []byte(content), 0o644)
	require.NoError(t, err)

	cfg, err := ParseImportCfg(filename)
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

// errorReader is a reader that returns an error after reading some data
type errorReader struct {
	data []byte
	pos  int
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	if r.pos >= len(r.data) {
		return n, r.err
	}
	return n, nil
}

func TestParse_ScannerError(t *testing.T) {
	expectedErr := errors.New("read error")
	reader := &errorReader{
		data: []byte("packagefile fmt=/path/to/fmt.a\npackagefile strings="),
		err:  expectedErr,
	}

	_, err := parse(reader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scanning importcfg")
	assert.True(t, errors.Is(err, expectedErr) || errors.Is(errors.Unwrap(err), expectedErr) ||
		err.Error() == "scanning importcfg: read error", "error should wrap the read error")
}

func TestWrite_DeterministicOrder(t *testing.T) {
	cfg := ImportConfig{
		PackageFile: map[string]string{
			"strings":  "/path/to/strings.a",
			"fmt":      "/path/to/fmt.a",
			"net/http": "/path/to/net/http.a",
			"context":  "/path/to/context.a",
		},
		ImportMap: map[string]string{
			"example.com/z":   "example.com/z/v2",
			"example.com/a":   "example.com/a/v2",
			"example.com/pkg": "example.com/pkg/v2",
		},
	}

	// Write multiple times and verify output is identical
	var buf1, buf2, buf3 bytes.Buffer
	require.NoError(t, cfg.write(&buf1))
	require.NoError(t, cfg.write(&buf2))
	require.NoError(t, cfg.write(&buf3))

	output1 := buf1.String()
	output2 := buf2.String()
	output3 := buf3.String()

	assert.Equal(t, output1, output2, "output should be deterministic")
	assert.Equal(t, output1, output3, "output should be deterministic")

	// Verify imports are sorted alphabetically
	lines := bytes.Split(buf1.Bytes(), []byte("\n"))
	var importMapLines, packageFileLines []string
	for _, line := range lines {
		lineStr := string(line)
		if bytes.HasPrefix(line, []byte("importmap ")) {
			importMapLines = append(importMapLines, lineStr)
		} else if bytes.HasPrefix(line, []byte("packagefile ")) {
			packageFileLines = append(packageFileLines, lineStr)
		}
	}

	// Check importmap is sorted
	assert.Equal(t, []string{
		"importmap example.com/a=example.com/a/v2",
		"importmap example.com/pkg=example.com/pkg/v2",
		"importmap example.com/z=example.com/z/v2",
	}, importMapLines)

	// Check packagefile is sorted
	assert.Equal(t, []string{
		"packagefile context=/path/to/context.a",
		"packagefile fmt=/path/to/fmt.a",
		"packagefile net/http=/path/to/net/http.a",
		"packagefile strings=/path/to/strings.a",
	}, packageFileLines)
}
