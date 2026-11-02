// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/gitcmd"
)

const (
	// attributesContent is the local attributes file for a merge. It blocks
	// contributor merge drivers, filters, and bundle conversion.
	attributesContent = "* !merge -filter\n" +
		"tool/data/otelc-bundle.tgz merge=otelc-bundle -text -ident !working-tree-encoding !eol\n"

	mergeUsage         = "Usage: bundlerepair merge <checkout> <base-sha>"
	attributesDirMode  = 0o755
	attributesFileMode = 0o644
)

// merge merges baseSHA into the checkout without committing. The otelc-bundle
// merge driver keeps the current archive. Source conflicts stay conflicts.
func (a app) merge(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "--help" {
		_, err := fmt.Fprintln(a.stdout, mergeUsage)
		return err
	}
	if len(args) != 2 || args[0] == "" || !sha1Pattern.MatchString(args[1]) {
		return errors.New("a checkout and a full base commit SHA are required")
	}
	return mergeBase(ctx, args[0], args[1], a.stdout)
}

// mergeBase writes the trusted local attributes and merges baseSHA into the
// checkout without committing.
func mergeBase(ctx context.Context, checkout, baseSHA string, stdout io.Writer) error {
	git := gitcmd.Runner{
		Dir:    checkout,
		Config: []string{"merge.otelc-bundle.driver=true"},
		Stdout: stdout,
	}
	if err := writeLocalAttributes(ctx, git); err != nil {
		return err
	}
	return git.Run(ctx, "merge", "--no-ff", "--no-commit", baseSHA)
}

func writeLocalAttributes(ctx context.Context, git gitcmd.Runner) error {
	out, err := git.Output(ctx, "rev-parse", "--path-format=absolute", "--git-path", "info/attributes")
	if err != nil {
		return err
	}
	path := strings.TrimSuffix(out, "\n")
	if !filepath.IsAbs(path) {
		return errors.New("git returned the non-absolute attributes path " + strconv.Quote(path))
	}
	if err = os.MkdirAll(filepath.Dir(path), attributesDirMode); err != nil {
		return errs.Wrap(err, "create the attributes directory")
	}
	if err = os.WriteFile(path, []byte(attributesContent), attributesFileMode); err != nil {
		return errs.Wrap(err, "write "+path)
	}
	return nil
}
