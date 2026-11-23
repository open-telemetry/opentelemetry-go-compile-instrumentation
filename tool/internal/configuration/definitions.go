// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.yaml.in/yaml/v3"
)

const (
	OpenTelemetryConfigurationRepository = "open-telemetry/opentelemetry-configuration"
	// OpenTelemetryConfigurationRelease and OpenTelemetryConfigurationCommit pin
	// the schema source used to generate catalog.yaml.
	OpenTelemetryConfigurationRelease = "v1.2.0"
	OpenTelemetryConfigurationCommit  = "3b04ae78576ec407854d734eb4da4f78a67f902c"
	OpenTelemetryConfigurationFile    = "schema/instrumentation.yaml"
	OpenTelemetryConfigurationURL     = "https://github.com/open-telemetry/opentelemetry-configuration/blob/" + OpenTelemetryConfigurationCommit + "/" + OpenTelemetryConfigurationFile

	// CatalogPath is the path relative to the otelc repository root. The Go
	// watcher reads this same file from the otelc checkout it is scanning.
	CatalogPath = "tool/internal/configuration/catalog.yaml"
)

//go:embed catalog.yaml
var embeddedCatalog []byte

type catalogSource struct {
	Repository string `yaml:"repository"`
	Release    string `yaml:"release"`
	Commit     string `yaml:"commit"`
	File       string `yaml:"file"`
}

type catalogDocument struct {
	CatalogVersion int              `yaml:"catalog_version"`
	Source         catalogSource    `yaml:"source"`
	Definitions    map[string]Entry `yaml:"definitions"`
}

// CatalogDefinitions loads the checked-in catalog without network access.
// general.* entries are derived from the pinned OpenTelemetry Configuration
// release; otelc-owned go.* entries can be added to this catalog as they are
// introduced.
func CatalogDefinitions() (map[string]Entry, error) {
	return ParseCatalog(embeddedCatalog)
}

// ParseCatalog parses a checked-in catalog of definitions keyed by ref. Refs
// are the canonical declarative paths, so both the watcher and otelc use the
// same identifier without a Java-specific alias layer.
func ParseCatalog(data []byte) (map[string]Entry, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document catalogDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, ex.Wrapf(err, "parse configuration catalog")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, ex.New("configuration catalog must contain a single YAML document")
		}
		return nil, ex.Wrapf(err, "parse trailing configuration catalog document")
	}

	if document.CatalogVersion != 1 {
		return nil, ex.New("unsupported configuration catalog version; expected 1")
	}
	if document.Source == (catalogSource{}) {
		return nil, ex.New("configuration catalog source is required")
	}
	if document.Source.Repository != OpenTelemetryConfigurationRepository ||
		document.Source.Release != OpenTelemetryConfigurationRelease ||
		document.Source.Commit != OpenTelemetryConfigurationCommit ||
		document.Source.File != OpenTelemetryConfigurationFile {
		return nil, ex.Newf(
			"configuration catalog source must pin %s at %s",
			OpenTelemetryConfigurationRelease,
			OpenTelemetryConfigurationCommit,
		)
	}
	if len(document.Definitions) == 0 {
		return nil, ex.New("configuration catalog has no definitions")
	}
	refs := make([]string, 0, len(document.Definitions))
	for ref := range document.Definitions {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	definitions := document.Definitions
	for _, ref := range refs {
		source := fmt.Sprintf("configuration catalog ref %q", ref)
		entry := definitions[ref]
		if entry.Ref != "" {
			return nil, ex.Newf("%s: catalog definitions must use name, not another ref", source)
		}
		if ref != entry.Name {
			return nil, ex.Newf("%s: catalog key must equal name %q", source, entry.Name)
		}
		if !strings.HasPrefix(ref, "general.") && !strings.HasPrefix(ref, "go.") {
			return nil, ex.Newf("%s: catalog refs must use the general. or go. namespace", source)
		}
		if validateErr := validateEntry(entry, source); validateErr != nil {
			return nil, validateErr
		}
		if strings.HasPrefix(ref, "general.") {
			entry.Source = source + " from " + OpenTelemetryConfigurationURL
		} else {
			entry.Source = source + " (otelc-owned go.* definition)"
		}
		definitions[ref] = entry
	}
	return definitions, nil
}
