// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
)

// namedError is a value type, so it exercises the qualified-name branch.
type namedError struct{}

func (namedError) Error() string { return "named" }

func TestErrorType(t *testing.T) {
	pkgPath := reflect.TypeFor[namedError]().PkgPath()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error falls back to _OTHER",
			err:  nil,
			want: "_OTHER",
		},
		{
			name: "unnamed pointer error uses its printed type",
			err:  errors.New("boom"),
			want: "*errors.errorString",
		},
		{
			name: "named error uses its fully qualified type",
			err:  namedError{},
			want: pkgPath + ".namedError",
		},
		{
			name: "wrapped error reports the outermost type",
			err:  &net.OpError{Op: "dial", Err: errors.New("refused")},
			want: "*net.OpError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorType(tt.err)
			if got.Key != ErrorTypeKey {
				t.Errorf("key = %q, want %q", got.Key, ErrorTypeKey)
			}
			if got.Value.AsString() != tt.want {
				t.Errorf("value = %q, want %q", got.Value.AsString(), tt.want)
			}
		})
	}
}

// A typed-nil error is non-nil as an interface but has no usable dynamic value;
// ErrorType must classify it rather than panic.
func TestErrorTypeTypedNil(t *testing.T) {
	var typed *net.OpError
	var err error = typed

	got := ErrorType(err)
	if got.Key != ErrorTypeKey {
		t.Fatalf("key = %q, want %q", got.Key, ErrorTypeKey)
	}
	if !strings.Contains(got.Value.AsString(), "net.OpError") {
		t.Errorf("value = %q, want it to name net.OpError", got.Value.AsString())
	}
}
