// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"maps"
	"slices"

	"github.com/invopop/jsonschema"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

type ruleSpec struct {
	typeName   string
	structInst any

	title string

	whereFields []string

	modifierName   string
	modifierFields []string
}

// schemaBuilder holds shared state needed while building the JSON Schema.
type schemaBuilder struct {
	reflector   *jsonschema.Reflector
	definitions jsonschema.Definitions
}

func newSchemaBuilder() (*schemaBuilder, error) {
	reflector := &jsonschema.Reflector{}
	// Use a fake base path so that when jsonschema walks the repo root ("../../..")
	// the computed import paths correctly resolve to "go.opentelemetry.io/otelc/...".
	if err := reflector.AddGoComments("go.opentelemetry.io/otelc/a/b/c", "../../.."); err != nil {
		return nil, err
	}

	b := &schemaBuilder{
		reflector:   reflector,
		definitions: jsonschema.Definitions{},
	}

	// Reflect InstBaseRule to extract top-level properties for the unified rule.
	baseSchema := b.reflector.Reflect(&rule.InstBaseRule{})
	maps.Copy(b.definitions, baseSchema.Definitions)

	if baseDef, ok := b.definitions["InstBaseRule"]; ok {
		// Remove `where` from InstBaseRule. We explicitly define and constrain
		// `where` in the rule variants. This allows us to remove WhereDef completely.
		baseDef.Properties.Delete("where")
	}

	return b, nil
}

func getRuleSpecs() []ruleSpec {
	return []ruleSpec{
		{
			typeName:   "InstFuncRule",
			structInst: &rule.InstFuncRule{},
			title:      "Function Rule",
			whereFields: []string{
				"func",
				"recv",
				"signature",
				"signature_contains",
				"result",
				"last_result",
				"param",
			},
			modifierName:   "inject_hooks",
			modifierFields: []string{"before", "after", "path"},
		},
		{
			typeName:       "InstRawRule",
			structInst:     &rule.InstRawRule{},
			title:          "Raw Code Rule",
			whereFields:    []string{"func", "recv", "pattern", "placement"},
			modifierName:   "inject_code",
			modifierFields: []string{"raw"},
		},
		{
			typeName:       "InstStructRule",
			structInst:     &rule.InstStructRule{},
			title:          "Struct Rule",
			whereFields:    []string{"struct"},
			modifierName:   "add_struct_fields",
			modifierFields: []string{"new_field"},
		},
		{
			typeName:       "InstCallRule",
			structInst:     &rule.InstCallRule{},
			title:          "Call Rule",
			whereFields:    []string{"function_call"},
			modifierName:   "wrap_call",
			modifierFields: []string{"replace", "append_args", "variadic_type"},
		},
		{
			typeName:       "InstDirectiveRule",
			structInst:     &rule.InstDirectiveRule{},
			title:          "Directive Rule",
			whereFields:    []string{"directive"},
			modifierName:   "expand_directive",
			modifierFields: []string{"template"},
		},
		{
			typeName:       "InstFileRule",
			structInst:     &rule.InstFileRule{},
			title:          "File Rule",
			whereFields:    nil,
			modifierName:   "add_file",
			modifierFields: []string{"file", "path"},
		},
		{
			typeName:       "InstDeclRule",
			structInst:     &rule.InstDeclRule{},
			title:          "Declaration Rule",
			whereFields:    []string{"kind", "identifier"},
			modifierName:   "assign_value",
			modifierFields: []string{"replace", "wrap"},
		},
	}
}

// GenerateSchema generates the complete JSON Schema for otelc
// YAML rule files (e.g. otelc.yaml) using the original rule
// structs directly via reflection.
func GenerateSchema() (*jsonschema.Schema, error) {
	b, err := newSchemaBuilder()
	if err != nil {
		return nil, err
	}

	// Ensure FilterDef definition is generated for where.file references.
	filterDefSchema := b.reflector.Reflect(&rule.FilterDef{})
	maps.Copy(b.definitions, filterDefSchema.Definitions)

	specs := getRuleSpecs()

	whereSchemas := make([]*jsonschema.Schema, 0, len(specs))
	doSchemas := make([]*jsonschema.Schema, 0, len(specs))
	pairings := make([]*jsonschema.Schema, 0, len(specs))

	for _, spec := range specs {
		res := b.buildVariantSchemas(spec)
		whereSchemas = append(whereSchemas, res.whereRef)
		doSchemas = append(doSchemas, res.doRef)
		pairings = append(pairings, res.pair)
	}

	// Remove full-struct definitions that are only used during reflection
	// but never referenced by any variant schema.
	for _, spec := range specs {
		delete(b.definitions, spec.typeName)
	}

	// Remove WhereDef as it is an implementation detail and we
	// now restrict where clauses using the concrete rule types.
	delete(b.definitions, "WhereDef")

	unifiedRule := &jsonschema.Schema{
		Title:       "Instrumentation Rule",
		Description: "A single otelc instrumentation rule.",
		Type:        "object",
		AllOf: []*jsonschema.Schema{
			{OneOf: pairings},
		},
		Properties: jsonschema.NewProperties(),
	}

	// Copy base properties to the unified rule to preserve exact ordering:
	// name, target, version, imports, where, do.
	if baseDef, ok := b.definitions["InstBaseRule"]; ok {
		for pair := baseDef.Properties.Oldest(); pair != nil; pair = pair.Next() {
			unifiedRule.Properties.Set(pair.Key, pair.Value)
		}
		unifiedRule.Required = append(unifiedRule.Required, baseDef.Required...)
	}

	unifiedRule.Properties.Set("where", &jsonschema.Schema{
		Description: "Rule matching conditions. Available fields depend on the rule type.",
		AnyOf:       whereSchemas,
	})
	unifiedRule.Properties.Set("do", &jsonschema.Schema{
		Description: "Rule actions to perform when matched. Available actions depend on the rule type.",
		AnyOf:       doSchemas,
	})

	// Remove InstBaseRule since its properties are now directly on the unified rule.
	delete(b.definitions, "InstBaseRule")

	return &jsonschema.Schema{
		Version:              "https://json-schema.org/draft/2020-12/schema",
		Title:                "OpenTelemetry Go Compile Instrumentation Rule File",
		Description:          "Schema for otelc instrumentation rule files",
		Type:                 "object",
		AdditionalProperties: unifiedRule,
		Definitions:          b.definitions,
	}, nil
}

type variantSchemas struct {
	whereRef *jsonschema.Schema
	doRef    *jsonschema.Schema
	pair     *jsonschema.Schema
}

// buildVariantSchemas reflects on the original rule struct and splits its properties
// into where-selectors and do-modifiers based on the ruleSpec, then returns the references
// and pairing schema for the unified rule.
func (b *schemaBuilder) buildVariantSchemas(
	spec ruleSpec,
) variantSchemas {
	b.registerStruct(spec.structInst)

	// Capture the description before we delete the struct definition later
	structDef := b.definitions[spec.typeName]
	description := ""
	if structDef != nil {
		description = structDef.Description
	}

	b.splitProperties(spec)
	whereDefName := spec.typeName + "Where"
	doDefName := spec.typeName + "Do"
	return assembleVariantSchemas(spec, whereDefName, doDefName, description)
}

// registerStruct reflects the struct and merges its definitions into the shared map.
func (b *schemaBuilder) registerStruct(v any) {
	s := b.reflector.Reflect(v)
	maps.Copy(b.definitions, s.Definitions)
}

func copyObjectConstraints(dst, src *jsonschema.Schema, fields []string) {
	allowed := make(map[string]bool, len(fields))
	for _, f := range fields {
		allowed[f] = true
	}

	filterSlice := func(in []string) []string {
		out := slices.DeleteFunc(slices.Clone(in), func(s string) bool { return !allowed[s] })
		if len(out) == 0 {
			return nil
		}
		return out
	}

	filterMap := func(in map[string]*jsonschema.Schema) map[string]*jsonschema.Schema {
		var out map[string]*jsonschema.Schema
		for k, v := range in {
			if allowed[k] {
				if out == nil {
					out = make(map[string]*jsonschema.Schema)
				}
				out[k] = v
			}
		}
		return out
	}

	dst.MinProperties = src.MinProperties
	dst.MaxProperties = src.MaxProperties
	dst.PropertyNames = src.PropertyNames
	dst.PatternProperties = filterMap(src.PatternProperties)
	dst.DependentSchemas = filterMap(src.DependentSchemas)
	dst.Required = filterSlice(src.Required)

	var depReq map[string][]string
	for k, vals := range src.DependentRequired {
		if !allowed[k] {
			continue
		}
		if filtered := filterSlice(vals); len(filtered) > 0 {
			if depReq == nil {
				depReq = make(map[string][]string)
			}
			depReq[k] = filtered
		}
	}
	dst.DependentRequired = depReq
}

// splitProperties reads the reflected struct definition and splits its fields
// into where-selector and do-modifier sub-schemas registered as definitions.
func (b *schemaBuilder) splitProperties(spec ruleSpec) {
	structDef := b.definitions[spec.typeName]

	whereSet := make(map[string]bool)
	for _, f := range spec.whereFields {
		whereSet[f] = true
	}

	modSet := make(map[string]bool)
	for _, f := range spec.modifierFields {
		modSet[f] = true
	}

	whereProps := jsonschema.NewProperties()
	doProps := jsonschema.NewProperties()

	if structDef != nil && structDef.Properties != nil {
		for pair := structDef.Properties.Oldest(); pair != nil; pair = pair.Next() {
			if whereSet[pair.Key] {
				whereProps.Set(pair.Key, pair.Value)
			} else if modSet[pair.Key] {
				doProps.Set(pair.Key, pair.Value)
			}
		}
	}

	if _, exists := whereProps.Get("file"); !exists {
		whereProps.Set("file", &jsonschema.Schema{
			Ref:         "#/$defs/FilterDef",
			Description: "File-level predicates",
		})
	}

	whereSchema := &jsonschema.Schema{
		Type:                 "object",
		Description:          structDef.Description,
		Properties:           whereProps,
		AdditionalProperties: jsonschema.FalseSchema,
	}
	copyObjectConstraints(whereSchema, structDef, spec.whereFields)

	if spec.typeName == "InstRawRule" {
		if whereSchema.DependentRequired == nil {
			whereSchema.DependentRequired = make(map[string][]string)
		}
		whereSchema.DependentRequired["placement"] = []string{"pattern"}
	}

	whereDefName := spec.typeName + "Where"
	b.definitions[whereDefName] = whereSchema

	doSchema := &jsonschema.Schema{
		Type:                 "object",
		Description:          structDef.Description,
		Properties:           doProps,
		AdditionalProperties: jsonschema.FalseSchema,
	}
	copyObjectConstraints(doSchema, structDef, spec.modifierFields)

	doDefName := spec.typeName + "Do"
	b.definitions[doDefName] = doSchema
}

// assembleVariantSchemas builds the conditional pairing schemas.
func assembleVariantSchemas(
	spec ruleSpec,
	whereDefName, doDefName, description string,
) variantSchemas {
	itemSchema := &jsonschema.Schema{
		Type:                 "object",
		Properties:           jsonschema.NewProperties(),
		Required:             []string{spec.modifierName},
		AdditionalProperties: jsonschema.FalseSchema,
	}
	itemSchema.Properties.Set(spec.modifierName, &jsonschema.Schema{
		Ref:         "#/$defs/" + doDefName,
		Description: description,
	})

	doLen := uint64(1)
	doSchema := &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{Type: "array", MinItems: &doLen, Items: itemSchema},
			itemSchema,
		},
	}

	pair := &jsonschema.Schema{
		Title:      spec.title,
		Properties: jsonschema.NewProperties(),
		Required:   []string{"do"},
	}

	pair.Properties.Set("do", doSchema)

	whereRef := &jsonschema.Schema{Ref: "#/$defs/" + whereDefName}
	pair.Properties.Set("where", whereRef)

	if len(spec.whereFields) > 0 {
		pair.Required = append(pair.Required, "where")
	}

	return variantSchemas{
		whereRef: whereRef,
		doRef:    doSchema,
		pair:     pair,
	}
}

// GenerateSchemaJSON returns the indented JSON byte slice representation of the rule schema.
func GenerateSchemaJSON() ([]byte, error) {
	s, err := GenerateSchema()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(s, "", "  ")
}
