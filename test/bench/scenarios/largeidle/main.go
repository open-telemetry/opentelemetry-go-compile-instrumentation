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
// Every imported package is actively used to prevent dead-code elimination
// from optimizing away the dependency tree.
//
// Deliberately avoids: net/http, database/sql, google.golang.org/grpc,
// github.com/redis/go-redis, go.opentelemetry.io/*.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"debug/dwarf"
	"debug/elf"
	"encoding/asn1"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"image"
	"image/jpeg"
	"image/png"
	"math/big"
	"mime/multipart"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
	"github.com/cespare/xxhash/v2"
	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	jsoniter "github.com/json-iterator/go"
	"github.com/klauspost/compress/zstd"
	"github.com/mitchellh/mapstructure"
	tomlv2 "github.com/pelletier/go-toml/v2"
	"github.com/rogpeppe/go-internal/txtar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tidwall/gjson"
	"go.uber.org/atomic"
	"go.uber.org/multierr"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/sha3"
	"golang.org/x/exp/maps"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

func main() {
	// stdlib: archive
	tw := tar.NewWriter(&bytes.Buffer{})
	_ = tw.Close()
	zr, _ := zip.NewReader(bytes.NewReader(nil), 0)
	_ = zr

	// stdlib: compress
	_ = bzip2.NewReader(strings.NewReader(""))
	gw := gzip.NewWriter(&bytes.Buffer{})
	_ = gw.Close()

	// stdlib: crypto
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	_ = key
	_, _, _ = ed25519.GenerateKey(rand.Reader)
	_ = &tls.Config{MinVersion: tls.VersionTLS12}
	_ = x509.NewCertPool()

	// stdlib: debug
	_ = new(dwarf.Data)
	_ = new(elf.File)

	// stdlib: encoding
	_, _ = asn1.Marshal(42)
	cw := csv.NewWriter(&bytes.Buffer{})
	cw.Flush()
	_ = hex.EncodeToString([]byte("bench"))
	_ = json.NewEncoder(&bytes.Buffer{})
	_ = pem.EncodeToMemory(&pem.Block{Type: "BENCH"})
	_ = xml.NewEncoder(&bytes.Buffer{})

	// stdlib: go/*
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", "package p", parser.AllErrors)
	ast.Walk(nil, f)
	_ = printer.Fprint(&bytes.Buffer{}, fset, f)

	// stdlib: image
	_ = image.NewRGBA(image.Rect(0, 0, 1, 1))
	_ = jpeg.DefaultQuality
	_ = png.DefaultCompression

	// stdlib: math/big
	_ = new(big.Int).SetInt64(1)

	// stdlib: mime/multipart
	mw := multipart.NewWriter(&bytes.Buffer{})
	_ = mw.FormDataContentType()

	// stdlib: os/exec
	_ = exec.Command("true")

	// stdlib: regexp
	_ = regexp.MustCompile(`\d+`)

	// stdlib: text/template
	_ = template.Must(template.New("bench").Parse("{{ . }}"))

	// third-party: BurntSushi/toml
	var tomlVal map[string]any
	_, _ = toml.Decode("[section]\nkey = \"val\"", &tomlVal)

	// third-party: cespare/xxhash
	_ = xxhash.Sum64String("bench")

	// third-party: davecgh/go-spew
	_ = spew.Sdump("bench")

	// third-party: google/go-cmp
	_ = cmp.Equal(1, 1)

	// third-party: google/uuid
	_ = uuid.New()

	// third-party: hashicorp/go-multierror
	_ = multierror.Append(nil, fmt.Errorf("bench"))

	// third-party: json-iterator/go
	_ = jsoniter.ConfigDefault

	// third-party: klauspost/compress/zstd
	enc, _ := zstd.NewWriter(nil)
	_ = enc

	// third-party: mitchellh/mapstructure
	var out struct{}
	_ = mapstructure.Decode(map[string]any{}, &out)

	// third-party: pelletier/go-toml/v2
	_ = tomlv2.NewDecoder(strings.NewReader(""))

	// third-party: rogpeppe/go-internal/txtar
	_ = txtar.Parse([]byte("-- file --\ncontent"))

	// third-party: spf13/cobra
	_ = &cobra.Command{Use: "bench"}

	// third-party: spf13/pflag
	fs := pflag.NewFlagSet("bench", pflag.ContinueOnError)
	_ = fs

	// third-party: tidwall/gjson
	_ = gjson.Get(`{"key":"val"}`, "key")

	// third-party: go.uber.org/atomic
	av := atomic.NewInt64(0)
	_ = av.Load()

	// third-party: go.uber.org/multierr
	_ = multierr.Combine(nil, nil)

	// third-party: golang.org/x/crypto
	_, _ = bcrypt.GenerateFromPassword([]byte("bench"), bcrypt.MinCost)
	h := sha3.New256()
	_ = h

	// third-party: golang.org/x/exp/maps
	_ = maps.Keys(map[string]int{"a": 1})

	// third-party: golang.org/x/text
	_ = language.English
	_ = norm.NFC.String("bench")

	// third-party: gopkg.in/yaml.v3
	var yamlVal any
	_ = yaml.Unmarshal([]byte("key: val"), &yamlVal)

	os.Exit(0)
}
