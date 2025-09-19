// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import "strings"

type Advice struct {
	Before string `json:"before" yaml:"before"`
	After  string `json:"after"  yaml:"after"`
}

type InstFuncRule struct {
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	Path     string   `json:"path"           yaml:"path"`
	Pointcut string   `json:"pointcut"       yaml:"pointcut"`
	Advice   []Advice `json:"advice"         yaml:"advice"`
}

func (r *InstFuncRule) String() string {
	return r.Name
}

func (r *InstFuncRule) GetPath() string {
	return r.Path
}

func (r *InstFuncRule) GetName() string {
	return r.Name
}

func (r *InstFuncRule) GetFuncName() string {
	return strings.Split(r.Pointcut, ".")[1]
}

func (r *InstFuncRule) GetFuncImportPath() string {
	return strings.Split(r.Pointcut, ".")[0]
}

func (r *InstFuncRule) GetBeforeAdvice() string {
	for _, advice := range r.Advice {
		if advice.Before != "" {
			return advice.Before
		}
	}
	return ""
}

func (r *InstFuncRule) GetAfterAdvice() string {
	for _, advice := range r.Advice {
		if advice.After != "" {
			return advice.After
		}
	}
	return ""
}
