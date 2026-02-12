// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package imports

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

// ImportConfig represents the parsed contents of an importcfg (or importcfg.link) file,
// usually passed to the Go compiler and linker via the -importcfg flag.
type ImportConfig struct {
	// PackageFile maps package import paths to their build archive locations
	PackageFile map[string]string
	// ImportMap maps package import paths to their fully-qualified versions
	ImportMap map[string]string
	// Extras contains unparsed lines that should be preserved when writing
	Extras []string
}

// ParseImportCfg parses the contents of the provided importcfg (or importcfg.link) file.
func ParseImportCfg(filename string) (ImportConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		return ImportConfig{}, err
	}
	defer file.Close()

	return parse(file)
}

// parse parses the importcfg data from the provided reader.
func parse(r io.Reader) (ImportConfig, error) {
	var reg ImportConfig
	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}

		directive, data, found := strings.Cut(line, " ")
		if !found {
			reg.Extras = append(reg.Extras, line)
			continue
		}

		switch directive {
		case "packagefile":
			importPath, archive, hasEq := strings.Cut(data, "=")
			if !hasEq {
				reg.Extras = append(reg.Extras, line)
				continue
			}

			if reg.PackageFile == nil {
				reg.PackageFile = make(map[string]string)
			}
			reg.PackageFile[importPath] = archive

		case "importmap":
			importPath, mappedTo, hasEq := strings.Cut(data, "=")
			if !hasEq {
				reg.Extras = append(reg.Extras, line)
				continue
			}

			if reg.ImportMap == nil {
				reg.ImportMap = make(map[string]string)
			}
			reg.ImportMap[importPath] = mappedTo

		default:
			reg.Extras = append(reg.Extras, line)
		}
	}

	// Check for scanner errors after the loop
	if err := scanner.Err(); err != nil {
		return reg, ex.Wrapf(err, "scanning importcfg")
	}

	return reg, nil
}

// WriteFile writes the content of the ImportConfig to the provided file,
// in the format expected by the Go toolchain commands.
func (r *ImportConfig) WriteFile(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	return r.write(file)
}

// write writes the content of the ImportConfig to the provided writer,
// in the format expected by the Go toolchain commands.
func (r *ImportConfig) write(w io.Writer) error {
	// Sort importmap keys
	importMapKeys := make([]string, 0, len(r.ImportMap))
	for name := range r.ImportMap {
		importMapKeys = append(importMapKeys, name)
	}
	sort.Strings(importMapKeys)

	for _, name := range importMapKeys {
		if _, err := fmt.Fprintf(w, "importmap %s=%s\n", name, r.ImportMap[name]); err != nil {
			return err
		}
	}

	// Sort packagefile keys
	packageFileKeys := make([]string, 0, len(r.PackageFile))
	for name := range r.PackageFile {
		packageFileKeys = append(packageFileKeys, name)
	}
	sort.Strings(packageFileKeys)

	for _, name := range packageFileKeys {
		if _, err := fmt.Fprintf(w, "packagefile %s=%s\n", name, r.PackageFile[name]); err != nil {
			return err
		}
	}

	for _, data := range r.Extras {
		if _, err := fmt.Fprintf(w, "%s\n", data); err != nil {
			return err
		}
	}

	return nil
}
