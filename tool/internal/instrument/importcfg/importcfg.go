// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package importcfg provides utilities to parse and manipulate importcfg files
// used by the Go toolchain during compilation.
package importcfg

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
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

// ParseFile parses the contents of the provided importcfg (or importcfg.link) file.
func ParseFile(filename string) (ImportConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		return ImportConfig{}, err
	}
	defer file.Close()

	return parse(file)
}

// parse parses the importcfg data from the provided reader.
func parse(r io.Reader) (reg ImportConfig, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		if err = scanner.Err(); err != nil {
			return
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}

		directive, data, ok := strings.Cut(line, " ")
		if !ok {
			reg.Extras = append(reg.Extras, line)
			continue
		}

		switch directive {
		case "packagefile":
			importPath, archive, ok := strings.Cut(data, "=")
			if !ok {
				reg.Extras = append(reg.Extras, line)
				continue
			}

			if reg.PackageFile == nil {
				reg.PackageFile = make(map[string]string)
			}
			reg.PackageFile[importPath] = archive

		case "importmap":
			importPath, mappedTo, ok := strings.Cut(data, "=")
			if !ok {
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

	return
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
	for name, path := range r.ImportMap {
		_, err := fmt.Fprintf(w, "importmap %s=%s\n", name, path)
		if err != nil {
			return err
		}
	}

	for name, path := range r.PackageFile {
		_, err := fmt.Fprintf(w, "packagefile %s=%s\n", name, path)
		if err != nil {
			return err
		}
	}

	for _, data := range r.Extras {
		_, err := fmt.Fprintf(w, "%s\n", data)
		if err != nil {
			return err
		}
	}

	return nil
}
