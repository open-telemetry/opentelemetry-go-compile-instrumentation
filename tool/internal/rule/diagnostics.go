// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MatchDecision is one matcher decision about a rule. An empty Reason means
// the rule matched.
type MatchDecision struct {
	Phase   string
	Package string
	Source  string
	Rule    InstRule
	Reason  string
}

// WriteMatchDecision appends a real matcher decision to the experimental debug
// stream in dir. Callers must gate this on debug mode and serialize setup
// appends; toolexec processes use separate streams per source file.
func WriteMatchDecision(dir string, d MatchDecision) error {
	row := struct {
		Package string `json:"package"`
		Rule    string `json:"rule"`
		Phase   string `json:"phase"`
		Source  string `json:"source,omitempty"`
		Matched bool   `json:"matched"`
		Reason  string `json:"reason,omitempty"`
	}{d.Package, d.Rule.GetName(), d.Phase, d.Source, d.Reason == "", d.Reason}
	data, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := "setup.matches.jsonl"
	if d.Phase == "instrument" {
		name = filepath.Base(d.Source) + ".matches.jsonl"
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
