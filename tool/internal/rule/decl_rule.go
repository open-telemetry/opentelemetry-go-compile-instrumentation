// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstDeclRule represents a rule that matches a named top-level declaration
// (function, type, variable, or constant) and applies an action to it.
//
// Exactly one of value or wrap must be set.
//
// Example YAML (replace):
//
//	assign_default_transport:
//	  target: net/http
//	  kind: var
//	  identifier: DefaultTransport
//	  value: |
//	    &http.Transport{MaxIdleConns: 100}
//
// Example YAML (wrap):
//
//	wrap_default_transport:
//	  target: net/http
//	  kind: var
//	  identifier: DefaultTransport
//	  wrap: "otelhttp.NewTransport({{ . }})"
//	  imports:
//	    otelhttp: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
type InstDeclRule struct {
	InstBaseRule `yaml:",inline"`

	// Kind optionally constrains the kind of declaration to match.
	// Valid values: "func", "var", "const", "type", or "" (match any).
	Kind string `json:"kind" yaml:"kind"` // empty = matches any kind

	// Identifier is the name of the top-level declaration to match.
	Identifier string `json:"identifier" yaml:"identifier"`

	// Value is a Go expression to assign as the value of the matched var or
	// const declaration. Mutually exclusive with Wrap.
	Value string `json:"value" yaml:"value"`

	// Wrap wraps the existing initializer of the matched var or const
	// declaration using a template. {{ . }} is substituted with the original
	// expression. Mutually exclusive with Value. An error is returned at
	// instrumentation time if the declaration has no initializer.
	Wrap string `json:"wrap,omitempty" yaml:"wrap,omitempty"`
}

// NewInstDeclRule loads and validates an InstDeclRule from YAML data.
func NewInstDeclRule(data []byte, name string) (*InstDeclRule, error) {
	var r InstDeclRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid decl rule %q", name)
	}
	return &r, nil
}

// validDeclKinds lists accepted values for the kind field.
// An empty string ("") means match any kind.
var validDeclKinds = map[string]bool{ //nolint:gochecknoglobals // private lookup table
	"":      true, // match any
	"func":  true,
	"var":   true,
	"const": true,
	"type":  true,
}

func (r *InstDeclRule) validate() error {
	if strings.TrimSpace(r.Identifier) == "" {
		return ex.Newf("identifier cannot be empty")
	}
	if !validDeclKinds[r.Kind] {
		return ex.Newf("kind %q is invalid; must be one of: func, var, const, type, or empty", r.Kind)
	}

	hasValue := strings.TrimSpace(r.Value) != ""
	hasWrap := strings.TrimSpace(r.Wrap) != ""

	if r.Kind == "func" || r.Kind == "type" {
		if hasValue {
			return ex.Newf("value is not valid when kind is %q", r.Kind)
		}
		if hasWrap {
			return ex.Newf("wrap is not valid when kind is %q", r.Kind)
		}
		return ex.Newf("kind %q has no supported advice; use var or const to assign or wrap a value", r.Kind)
	}

	if !hasValue && !hasWrap {
		return ex.Newf("one of value or wrap must be set")
	}
	if hasValue && hasWrap {
		return ex.Newf("value and wrap are mutually exclusive")
	}

	if hasWrap {
		if !templatePlaceholderPattern.MatchString(r.Wrap) {
			return ex.Newf(
				"wrap template must contain {{ . }} placeholder (also accepts {{.}}, {{- . -}}, etc.)",
			)
		}
	}

	return nil
}
