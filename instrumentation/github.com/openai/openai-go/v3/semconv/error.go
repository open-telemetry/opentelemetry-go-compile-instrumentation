// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel/attribute"
)

// ErrorTypeKey describes a class of error the operation ended with.
const ErrorTypeKey = attribute.Key("error.type")

// errorTypeOther is the value used when no more specific type is available.
const errorTypeOther = "_OTHER"

// ErrorType returns the error.type attribute for err, derived from the error's
// fully qualified type name. This mirrors the convention the net/http
// instrumentation uses, so error.type stays consistent across instrumentations.
//
// GenAI semantic conventions make error.type conditionally required whenever an
// operation ends in an error, and RecordError only adds an exception event, so
// callers must set this attribute explicitly.
func ErrorType(err error) attribute.KeyValue {
	if err == nil {
		return ErrorTypeKey.String(errorTypeOther)
	}

	t := reflect.TypeOf(err)
	if t == nil {
		return ErrorTypeKey.String(errorTypeOther)
	}

	var value string
	if t.PkgPath() == "" && t.Name() == "" {
		// Likely a builtin type.
		value = t.String()
	} else {
		value = fmt.Sprintf("%s.%s", t.PkgPath(), t.Name())
	}

	if value == "" {
		return ErrorTypeKey.String(errorTypeOther)
	}

	return ErrorTypeKey.String(value)
}
