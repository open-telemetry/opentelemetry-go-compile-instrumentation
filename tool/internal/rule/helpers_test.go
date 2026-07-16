// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The helpers below let the per-rule constructor tests keep their historical
// flat-shaped fixtures while exercising the structured constructors. Production
// parsing only accepts the where/do shape (see ParseRules); these split a flat
// field map into the base fields, the where-selector node, and the modifier
// payload the constructors now take.

// flatMap unmarshals a flat rule YAML snippet into a field map, surfacing a
// syntax error so invalid-YAML fixtures still assert a failure.
func flatMap(yamlStr string) (map[string]any, error) {
	var m map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// mapNode renders a where-selector map into the mapping node the structured
// constructors decode selectors from. Returns nil for an empty map, which the
// constructors treat as "no where clause".
func mapNode(t *testing.T, m map[string]any) *yaml.Node {
	t.Helper()
	if len(m) == 0 {
		return nil
	}
	b, err := yaml.Marshal(m)
	require.NoError(t, err)
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(b, &doc))
	require.NotEmpty(t, doc.Content)
	return doc.Content[0]
}

// splitBase pulls the package-scope base fields out of a flat field map,
// returning the base and the remaining (where/do) keys. name is the default
// rule name; an explicit non-empty name field overrides it.
func splitBase(flat map[string]any, name string) (InstBaseRule, map[string]any) {
	base := InstBaseRule{Name: name}
	rest := make(map[string]any, len(flat))
	for k, v := range flat {
		switch k {
		case "target":
			base.Target, _ = v.(string)
		case "version":
			base.Version, _ = v.(string)
		case "name":
			if s, ok := v.(string); ok && s != "" {
				base.Name = s
			}
		case "imports":
			base.Imports = toStringMap(v)
		default:
			rest[k] = v
		}
	}
	return base, rest
}

func toStringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, vv := range m {
		out[k], _ = vv.(string)
	}
	return out
}

func toStrings(v any) []string {
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		out := make([]string, len(xs))
		for i, x := range xs {
			out[i], _ = x.(string)
		}
		return out
	default:
		return nil
	}
}
