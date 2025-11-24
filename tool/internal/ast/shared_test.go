// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
)

func TestCloneTypeParams(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		assert.Nil(t, CloneTypeParams(nil))
	})

	t.Run("clones are independent instances with same content", func(t *testing.T) {
		testCases := []struct {
			name     string
			original *dst.FieldList
		}{
			{
				name: "single type parameter",
				original: &dst.FieldList{
					List: []*dst.Field{
						{Names: []*dst.Ident{Ident("T")}, Type: Ident("any")},
					},
				},
			},
			{
				name: "multiple type parameters",
				original: &dst.FieldList{
					List: []*dst.Field{
						{Names: []*dst.Ident{Ident("T")}, Type: Ident("any")},
						{Names: []*dst.Ident{Ident("U")}, Type: Ident("comparable")},
					},
				},
			},
			{
				name: "field with multiple names",
				original: &dst.FieldList{
					List: []*dst.Field{
						{Names: []*dst.Ident{Ident("T"), Ident("U")}, Type: Ident("any")},
					},
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cloned := CloneTypeParams(tc.original)
				assert.NotSame(t, tc.original, cloned)
				assert.Equal(t, tc.original, cloned)
			})
		}
	})

	t.Run("modifications to clone don't affect original", func(t *testing.T) {
		original := &dst.FieldList{
			List: []*dst.Field{
				{Names: []*dst.Ident{Ident("T")}, Type: Ident("any")},
			},
		}
		cloned := CloneTypeParams(original)

		cloned.List[0].Names[0].Name = "Modified"

		assert.Equal(t, "T", original.List[0].Names[0].Name)
		assert.Equal(t, "Modified", cloned.List[0].Names[0].Name)
	})
}
