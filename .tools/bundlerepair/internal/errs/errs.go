// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package errs adds context to errors. The module has no dependencies, so it
// cannot use the repository error library. Wrap keeps the cause for errors.Is
// and errors.As.
package errs

// Wrap returns err with the context text in front. A nil err stays nil.
func Wrap(err error, context string) error {
	if err == nil {
		return nil
	}
	return &wrapped{context: context, err: err}
}

type wrapped struct {
	context string
	err     error
}

func (w *wrapped) Error() string { return w.context + ": " + w.err.Error() }

func (w *wrapped) Unwrap() error { return w.err }
