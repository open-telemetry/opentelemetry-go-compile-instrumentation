// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package errs_test

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
)

func TestWrap(t *testing.T) {
	_, cause := os.Open("/does/not/exist")
	err := errs.Wrap(cause, "open config")
	if got, want := err.Error(), "open config: "+cause.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("Wrap hid the cause from errors.Is")
	}
	if _, ok := errors.AsType[*fs.PathError](err); !ok {
		t.Error("Wrap hid the cause from errors.AsType")
	}
	if errs.Wrap(nil, "context") != nil {
		t.Error("Wrap(nil) returned an error")
	}
}
