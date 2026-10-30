// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package ghaction writes the files that GitHub Actions reads after a step:
// GITHUB_OUTPUT for step outputs and GITHUB_STEP_SUMMARY for the job summary.
package ghaction

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
)

const (
	// OutputEnv names the variable that holds the step output file path.
	OutputEnv = "GITHUB_OUTPUT"
	// SummaryEnv names the variable that holds the step summary file path.
	SummaryEnv = "GITHUB_STEP_SUMMARY"

	fileMode = 0o600
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Files holds the paths of the workflow command files.
type Files struct {
	Output  string
	Summary string
}

// FromEnv reads the file paths with getenv.
func FromEnv(getenv func(string) string) Files {
	return Files{Output: getenv(OutputEnv), Summary: getenv(SummaryEnv)}
}

// SetOutput appends the line "key=value" to the output file. The value must be
// one line. A multi-line value would let the value inject more outputs.
func (f Files) SetOutput(key, value string) error {
	if f.Output == "" {
		return errors.New(OutputEnv + " is not set")
	}
	if !keyPattern.MatchString(key) {
		return errors.New("invalid output name " + strconv.Quote(key))
	}
	if err := rejectNewline(value); err != nil {
		return errs.Wrap(err, "output "+key)
	}
	return appendLine(f.Output, key+"="+value)
}

// AppendSummary appends one line of Markdown to the step summary file.
func (f Files) AppendSummary(line string) error {
	if f.Summary == "" {
		return errors.New(SummaryEnv + " is not set")
	}
	if err := rejectNewline(line); err != nil {
		return errs.Wrap(err, "summary")
	}
	return appendLine(f.Summary, line)
}

func rejectNewline(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("value contains a line break")
	}
	return nil
}

func appendLine(path, line string) error {
	file, err := os.OpenFile(
		path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		fileMode,
	)
	if err != nil {
		return errs.Wrap(err, "open "+path)
	}
	_, writeErr := file.WriteString(line + "\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errs.Wrap(errors.Join(writeErr, closeErr), "write "+path)
	}
	return nil
}
