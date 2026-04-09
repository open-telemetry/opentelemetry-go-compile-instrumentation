// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is the large-idle benchmark scenario.
//
// It simulates a realistic production application with a large dependency tree
// (~250 compilation units) where none of the packages match any otelc
// instrumentation rule. Every compilation unit passes through the full
// per-unit overhead path (process spawn + JSON rule load + linear match scan)
// without triggering any AST rewriting.
//
// Deliberately avoids: net/http, database/sql, google.golang.org/grpc,
// github.com/redis/go-redis, go.opentelemetry.io/*.
package main

import (
	_ "archive/tar"
	_ "archive/zip"
	_ "compress/bzip2"
	_ "compress/gzip"
	_ "crypto/ecdsa"
	_ "crypto/ed25519"
	_ "crypto/rand"
	_ "crypto/tls"
	_ "crypto/x509"
	_ "debug/dwarf"
	_ "debug/elf"
	_ "encoding/asn1"
	_ "encoding/csv"
	_ "encoding/hex"
	_ "encoding/json"
	_ "encoding/pem"
	_ "encoding/xml"
	_ "go/ast"
	_ "go/parser"
	_ "go/printer"
	_ "go/token"
	_ "image"
	_ "image/jpeg"
	_ "image/png"
	_ "math/big"
	_ "mime/multipart"
	_ "os/exec"
	_ "regexp"
	_ "text/template"

	_ "github.com/BurntSushi/toml"
	_ "github.com/cespare/xxhash/v2"
	_ "github.com/davecgh/go-spew/spew"
	_ "github.com/google/go-cmp/cmp"
	_ "github.com/google/uuid"
	_ "github.com/hashicorp/go-multierror"
	_ "github.com/json-iterator/go"
	_ "github.com/klauspost/compress/zstd"
	_ "github.com/mitchellh/mapstructure"
	_ "github.com/pelletier/go-toml/v2"
	_ "github.com/rogpeppe/go-internal/txtar"
	_ "github.com/spf13/cobra"
	_ "github.com/spf13/pflag"
	_ "github.com/tidwall/gjson"
	_ "go.uber.org/atomic"
	_ "go.uber.org/multierr"
	_ "golang.org/x/crypto/bcrypt"
	_ "golang.org/x/crypto/sha3"
	_ "golang.org/x/exp/maps"
	_ "golang.org/x/text/language"
	_ "golang.org/x/text/unicode/norm"
	_ "gopkg.in/yaml.v3"
)

func main() {}
