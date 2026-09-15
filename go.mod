// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

module go.opentelemetry.io/otelc

go 1.26.0

require (
	github.com/bmatcuk/doublestar/v4 v4.10.0
	github.com/dave/dst v0.27.4
	github.com/gofrs/flock v0.13.1
	github.com/google/go-cmp v0.7.0
	github.com/stretchr/testify v1.12.1
	github.com/urfave/cli/v3 v3.11.0
	github.com/valyala/fasttemplate v1.2.2
	golang.org/x/mod v0.41.0
	golang.org/x/sync v0.23.0
	golang.org/x/sys v0.48.0
	golang.org/x/tools v0.50.0
	gotest.tools/v3 v3.5.2
)

require (
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5
)

retract v1.0.0 // otelc pin generates incorrect module paths in user go.mod files; use v1.0.1
