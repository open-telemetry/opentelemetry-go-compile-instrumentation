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
	// OpenTelemetryConfigurationRelease and OpenTelemetryConfigurationCommit pin
	// the schema snapshot from which the catalog's general.* definitions were
	// derived. Keep the catalog offline and reproducible; update these together
	// only when intentionally refreshing the checked-in catalog.
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
		return nil, ex.Newf("unsupported configuration catalog version %d", document.CatalogVersion)
	}
	if document.Source.Repository != "open-telemetry/opentelemetry-configuration" ||
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
	definitions := make(map[string]Entry, len(refs))
	for _, ref := range refs {
		entry := document.Definitions[ref]
		source := fmt.Sprintf("configuration catalog ref %q", ref)
		if ref == "" || entry.DeclarativeName == "" {
			return nil, ex.Newf("%s: ref and declarative_name must be non-empty", source)
		}
		if ref != entry.DeclarativeName {
			return nil, ex.Newf("%s: catalog key must equal declarative_name %q", source, entry.DeclarativeName)
		}
		if !strings.HasPrefix(ref, "general.") && !strings.HasPrefix(ref, "go.") {
			return nil, ex.Newf("%s: catalog refs must use the general. or go. namespace", source)
		}
		if err := validateEntry(entry, source); err != nil {
			return nil, err
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
