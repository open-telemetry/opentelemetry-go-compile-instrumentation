// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strconv"
	"strings"
)

type (
	ConventionalCommitError interface {
		error
		ReviewComment() string
	}

	InvalidFormatError struct{}
	InvalidCommitType  struct{ Type string }
)

func (InvalidFormatError) Error() string {
	return "title does not match expected conventional commit format"
}

func (InvalidFormatError) ReviewComment() string {
	return "The title of this pull request does not match the [conventional commits][cc-spec] format.\n" +
		"Please update the title, as apprioriate.\n\n" +
		"Refer to the [CONTRIBUTING.md][contributing] file for more information.\n\n" +
		"[cc-spec]: https://www.conventionalcommits.org/en/v1.0.0/\n" +
		"[contributing]: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/CONTRIBUTING.md#conventional-commits\n"
}

func (e InvalidCommitType) Error() string {
	return fmt.Sprintf("unsupported conventional commit type: %q", e.Type)
}

func (e InvalidCommitType) ReviewComment() string {
	var msg strings.Builder
	msg.WriteString("The title of this pull request uses an unsupported conventional commit type: ")
	msg.WriteString(strconv.Quote(e.Type))
	msg.WriteString(".\n")

	msg.WriteString("Please update the title to use one of the supported types:\n")
	for ctype := range conventionalLabels {
		msg.WriteString("- `")
		msg.WriteString(ctype)
		msg.WriteString("`\n")
	}

	return msg.String()
}
