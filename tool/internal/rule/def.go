// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"
)

type Advice struct {
	Before string `yaml:"before"`
	After  string `yaml:"after"`
}

type InstRule struct {
	Name     string   `yaml:"name,omitempty"`
	Path     string   `yaml:"path"`
	Pointcut string   `yaml:"pointcut"`
	Advice   []Advice `yaml:"advice"`
}

func (r *InstRule) String() string {
	return r.Name
}

func (r *InstRule) GetFuncName() string {
	return strings.Split(r.Pointcut, ".")[1]
}

func (r *InstRule) GetFuncImportPath() string {
	return strings.Split(r.Pointcut, ".")[0]
}
