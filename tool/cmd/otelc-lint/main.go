// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

func main() {
	app := cli.Command{
		Name:        "otelc-lint",
		Description: "Lint and validate OpenTelemetry Go Compile Instrumentation rule files",
		Commands: []*cli.Command{
			&commandSchema,
			&commandRules,
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

//nolint:gochecknoglobals // CLI command definition
var commandSchema = cli.Command{
	Name:        "schema",
	Description: "Output JSON Schema for otelc rule files to stdout",
	Action: func(_ context.Context, cmd *cli.Command) error {
		schemaBytes, err := GenerateSchemaJSON()
		if err != nil {
			return fmt.Errorf("generating schema: %w", err)
		}
		_, err = fmt.Fprintf(cmd.Writer, "%s\n", schemaBytes)
		return err
	},
}

//nolint:gochecknoglobals // CLI command definition
var commandRules = cli.Command{
	Name:        "rules",
	Description: "Validate rule files against the JSON Schema",
	Usage:       "rules <file|dir>...",
	Action: func(_ context.Context, cmd *cli.Command) error {
		args := cmd.Args().Slice()
		if len(args) == 0 {
			return errors.New("usage: otelc-lint rules <file|dir>")
		}

		schemaBytes, err := GenerateSchemaJSON()
		if err != nil {
			return fmt.Errorf("generating schema: %w", err)
		}

		schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
		if err != nil {
			return fmt.Errorf("unmarshalling schema: %w", err)
		}

		compiler := jsonschema.NewCompiler()
		if addErr := compiler.AddResource("schema.json", schemaDoc); addErr != nil {
			return fmt.Errorf("adding schema resource: %w", addErr)
		}

		compiledSchema, err := compiler.Compile("schema.json")
		if err != nil {
			return fmt.Errorf("compiling schema: %w", err)
		}

		var paths []string
		for _, arg := range args {
			info, statErr := os.Stat(arg)
			if statErr != nil {
				return fmt.Errorf("accessing %q: %w", arg, statErr)
			}
			if info.IsDir() {
				discovered, walkErr := discoverRuleFiles(arg)
				if walkErr != nil {
					return walkErr
				}
				paths = append(paths, discovered...)
			} else {
				paths = append(paths, arg)
			}
		}

		if len(paths) == 0 {
			return errors.New("no rule files found")
		}

		var failed int
		for _, p := range paths {
			if valErr := validateFile(compiledSchema, p); valErr != nil {
				_, _ = fmt.Fprintf(cmd.Writer, "FAIL %s\n%s\n", p, valErr)
				failed++
			} else {
				_, _ = fmt.Fprintf(cmd.Writer, "OK   %s\n", p)
			}
		}

		if failed > 0 {
			return fmt.Errorf("%d file(s) failed validation", failed)
		}
		return nil
	},
}

// discoverRuleFiles walks a directory and returns paths to
// otelc.yaml and *.otelc.yaml files.
func discoverRuleFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if isRuleFile(name) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func isRuleFile(name string) bool {
	if name == "otelc.yaml" || name == "otelc.yml" {
		return true
	}
	if strings.HasSuffix(name, ".otelc.yaml") || strings.HasSuffix(name, ".otelc.yml") {
		return true
	}
	return false
}

// validateFile reads a YAML file, converts it to JSON,
// and validates against the schema.
func validateFile(compiledSchema *jsonschema.Schema, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	var rawObj any
	if err = yaml.Unmarshal(content, &rawObj); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	// Convert YAML to JSON-compatible types via encoding/json round-trip.
	jsonBytes, err := json.Marshal(rawObj)
	if err != nil {
		return fmt.Errorf("marshalling to JSON: %w", err)
	}

	var val any
	if err = json.Unmarshal(jsonBytes, &val); err != nil {
		return fmt.Errorf("unmarshalling JSON: %w", err)
	}

	if err = compiledSchema.Validate(val); err != nil {
		return fmt.Errorf("schema validation failed:\n%w", err)
	}
	return nil
}
