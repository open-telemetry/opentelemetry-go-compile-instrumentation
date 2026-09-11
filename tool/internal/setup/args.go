// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"strings"
)

// ArgKind represents the semantic category of a command-line argument.
type ArgKind int

const (
	// ArgTarget represents a package pattern or file target (e.g., "./...", "main.go").
	ArgTarget ArgKind = iota

	// ArgBuildFlag represents a recognized Go build/command flag (e.g., "-tags=foo", "-race").
	ArgBuildFlag

	// ArgBuildFlagValue represents the separated value argument for an ArgBuildFlag.
	ArgBuildFlagValue

	// ArgTestFlag represents a recognized go test flag (e.g., "-run", "-test.run", "-v").
	ArgTestFlag

	// ArgTestFlagValue represents the separated value argument for an ArgTestFlag.
	ArgTestFlagValue

	// ArgTestDelimiter represents "--", "-args", or "--args" in go test.
	ArgTestDelimiter

	// ArgTestBinary represents test-binary arguments (tokens following a test delimiter,
	// or unknown/custom test flags and their values/positionals).
	ArgTestBinary

	// ArgFlagTerminator represents "--" in go build / go install.
	ArgFlagTerminator
)

// ClassifiedArg contains the classification metadata for a single command-line argument token.
type ClassifiedArg struct {
	Index    int
	Raw      string
	Kind     ArgKind
	FlagName string // normalized single-dash name, e.g. "-tags", "-run", "-test.run"
	HasValue bool   // whether joined =value was present
	Value    string // value from joined =value or separate token
}

// isBuildFlagWithValue reports whether name is a Go build flag that accepts a value.
func isBuildFlagWithValue(name string) bool {
	switch name {
	case "-C", "-o", "-p", "-covermode", "-coverpkg",
		"-asmflags", "-buildmode", "-buildvcs", "-compiler",
		"-gccgoflags", "-gcflags", "-installsuffix", "-ldflags",
		"-mod", "-modfile", "-overlay", "-pgo", "-pkgdir",
		"-tags", "-toolexec":
		return true
	default:
		return false
	}
}

// isBuildBoolFlag reports whether name is a Go build boolean flag.
func isBuildBoolFlag(name string) bool {
	switch name {
	case "-a", "-n", "-v", "-x", "-race", "-msan",
		"-asan", "-cover", "-trimpath", "-work", "-linkshared", "-json":
		return true
	default:
		return false
	}
}

// isTestFlagWithValue reports whether name is a go test flag that accepts a value.
func isTestFlagWithValue(name string) bool {
	switch name {
	case "-bench", "-benchtime", "-blockprofile", "-blockprofilerate",
		"-count", "-coverprofile", "-cpu", "-cpuprofile", "-exec",
		"-fuzz", "-fuzzminimizetime", "-fuzztime", "-list",
		"-memprofile", "-memprofilerate", "-mutexprofile",
		"-mutexprofilefraction", "-outputdir", "-parallel",
		"-run", "-shuffle", "-skip", "-timeout", "-trace", "-vet":
		return true
	default:
		return false
	}
}

// isTestBoolFlag reports whether name is a go test boolean flag distinct from buildBoolFlags.
func isTestBoolFlag(name string) bool {
	switch name {
	case "-artifacts", "-benchmem", "-c", "-failfast", "-fullpath", "-short":
		return true
	default:
		return false
	}
}

// isTestAliasFlagWithValue reports whether name is a supported -test.<name> alias taking a value.
// Derived from cmd/go/internal/test/flagdefs.go:passFlagToTest.
func isTestAliasFlagWithValue(name string) bool {
	switch name {
	case "-test.bench", "-test.benchtime", "-test.blockprofile", "-test.blockprofilerate",
		"-test.count", "-test.coverprofile", "-test.cpu", "-test.cpuprofile",
		"-test.fuzz", "-test.fuzzminimizetime", "-test.fuzztime", "-test.list",
		"-test.memprofile", "-test.memprofilerate", "-test.mutexprofile",
		"-test.mutexprofilefraction", "-test.outputdir", "-test.parallel",
		"-test.run", "-test.shuffle", "-test.skip", "-test.timeout", "-test.trace":
		return true
	default:
		return false
	}
}

// isTestAliasBoolFlag reports whether name is a supported -test.<name> boolean alias.
// Derived from cmd/go/internal/test/flagdefs.go:passFlagToTest.
func isTestAliasBoolFlag(name string) bool {
	switch name {
	case "-test.artifacts", "-test.benchmem", "-test.failfast",
		"-test.fullpath", "-test.short", "-test.v":
		return true
	default:
		return false
	}
}

// isBuildContextFlagWithValue reports whether name affects the build context and takes a value.
func isBuildContextFlagWithValue(name string) bool {
	switch name {
	case "-C", "-overlay", "-tags", "-mod", "-modfile":
		return true
	default:
		return false
	}
}

// isBuildContextBoolFlag reports whether name is a boolean flag affecting the build context.
func isBuildContextBoolFlag(name string) bool {
	switch name {
	case "-race", "-msan", "-cover", "-asan", "-trimpath":
		return true
	default:
		return false
	}
}

// isPlanIrrelevantFlag reports whether name is stripped from the build plan dry-run command.
func isPlanIrrelevantFlag(name string) bool {
	return name == flagJSON
}

// flagName returns the flag name of arg in single-dash form, without a joined value.
func flagName(arg string) string {
	name, _, _ := strings.Cut(arg, "=")
	if strings.HasPrefix(name, "--") {
		return name[1:]
	}
	return name
}

// isTestDelimiter reports whether arg is an explicit go test delimiter.
func isTestDelimiter(arg string) bool {
	return arg == delimiterDashDash || arg == flagArgs || arg == flagArgsDashDash
}

// takesValue reports whether a flag name requires an argument value.
func takesValue(name string) bool {
	return isBuildFlagWithValue(name) || isTestFlagWithValue(name) || isTestAliasFlagWithValue(name)
}

type flagToken struct {
	normName string
	hasValue bool
	value    string
}

func classifyValueFlag(
	args []string,
	i int,
	tok flagToken,
	isTest bool,
) (int, []ClassifiedArg) {
	flagKind := ArgBuildFlag
	valKind := ArgBuildFlagValue
	if isTest {
		flagKind = ArgTestFlag
		valKind = ArgTestFlagValue
	}
	if tok.hasValue {
		return 1, []ClassifiedArg{{
			Index:    i,
			Raw:      args[i],
			Kind:     flagKind,
			FlagName: tok.normName,
			HasValue: true,
			Value:    tok.value,
		}}
	}
	res := []ClassifiedArg{{
		Index:    i,
		Raw:      args[i],
		Kind:     flagKind,
		FlagName: tok.normName,
		HasValue: false,
	}}
	if i+1 < len(args) {
		res = append(res, ClassifiedArg{
			Index:    i + 1,
			Raw:      args[i+1],
			Kind:     valKind,
			FlagName: tok.normName,
			Value:    args[i+1],
		})
	}
	return len(res), res
}

type testArgState struct {
	inPkgList              bool
	packageListEstablished bool
	testArgsFinalized      bool
}

func classifyBuildFlagToken(args []string, i int) (int, []ClassifiedArg) {
	arg := args[i]
	rawName, value, hasValue := strings.Cut(arg, "=")
	tok := flagToken{
		normName: flagName(rawName),
		hasValue: hasValue,
		value:    value,
	}

	if isBuildFlagWithValue(tok.normName) {
		return classifyValueFlag(args, i, tok, false)
	}
	return 1, []ClassifiedArg{{
		Index:    i,
		Raw:      arg,
		Kind:     ArgBuildFlag,
		FlagName: tok.normName,
		HasValue: tok.hasValue,
		Value:    tok.value,
	}}
}

func classifyTestFlagToken(
	args []string,
	i int,
	state *testArgState,
) (int, []ClassifiedArg, bool, string) {
	arg := args[i]
	rawName, value, hasValue := strings.Cut(arg, "=")
	tok := flagToken{
		normName: flagName(rawName),
		hasValue: hasValue,
		value:    value,
	}

	if isBuildFlagWithValue(tok.normName) {
		c, it := classifyValueFlag(args, i, tok, false)
		return c, it, false, ""
	}
	if isBuildBoolFlag(tok.normName) {
		return 1, []ClassifiedArg{{
			Index:    i,
			Raw:      arg,
			Kind:     ArgBuildFlag,
			FlagName: tok.normName,
			HasValue: tok.hasValue,
			Value:    tok.value,
		}}, false, ""
	}
	if isTestFlagWithValue(tok.normName) || isTestAliasFlagWithValue(tok.normName) {
		c, it := classifyValueFlag(args, i, tok, true)
		return c, it, false, ""
	}
	if isTestBoolFlag(tok.normName) || isTestAliasBoolFlag(tok.normName) {
		return 1, []ClassifiedArg{{
			Index:    i,
			Raw:      arg,
			Kind:     ArgTestFlag,
			FlagName: tok.normName,
			HasValue: tok.hasValue,
			Value:    tok.value,
		}}, false, ""
	}

	// Unknown / custom flag: closes the package list region.
	state.packageListEstablished = true
	item := ClassifiedArg{
		Index:    i,
		Raw:      arg,
		Kind:     ArgTestBinary,
		FlagName: tok.normName,
		HasValue: tok.hasValue,
		Value:    tok.value,
	}
	return 1, []ClassifiedArg{item}, !tok.hasValue, tok.normName
}

func classifyPositionalTestArg(
	arg string,
	i int,
	state *testArgState,
	wasAfterUnknown bool,
	unknownName string,
) ClassifiedArg {
	if !state.inPkgList && state.packageListEstablished {
		if wasAfterUnknown {
			// Optimistically treated as the separated value for the preceding unknown flag.
			return ClassifiedArg{
				Index:    i,
				Raw:      arg,
				Kind:     ArgTestBinary,
				FlagName: unknownName,
				Value:    arg,
			}
		}

		// Definitive positional test argument. All remaining tokens become ArgTestBinary.
		state.testArgsFinalized = true
		return ClassifiedArg{
			Index: i,
			Raw:   arg,
			Kind:  ArgTestBinary,
		}
	}

	// Establishing or adding to the package list.
	state.inPkgList = true
	state.packageListEstablished = true
	return ClassifiedArg{
		Index: i,
		Raw:   arg,
		Kind:  ArgTarget,
	}
}

func classifyBuildArgs(args []string) []ClassifiedArg {
	classified := make([]ClassifiedArg, 0, len(args))
	flagParsingClosed := false

	for i := 0; i < len(args); {
		arg := args[i]
		if flagParsingClosed {
			classified = append(classified, ClassifiedArg{
				Index: i,
				Raw:   arg,
				Kind:  ArgTarget,
			})
			i++
			continue
		}

		if arg == delimiterDashDash {
			classified = append(classified, ClassifiedArg{
				Index: i,
				Raw:   arg,
				Kind:  ArgFlagTerminator,
			})
			flagParsingClosed = true
			i++
			continue
		}

		if strings.HasPrefix(arg, "-") {
			consumed, items := classifyBuildFlagToken(args, i)
			classified = append(classified, items...)
			i += consumed
			continue
		}

		classified = append(classified, ClassifiedArg{
			Index: i,
			Raw:   arg,
			Kind:  ArgTarget,
		})
		i++
	}

	return classified
}

func classifyTestArgs(args []string) []ClassifiedArg {
	classified := make([]ClassifiedArg, 0, len(args))
	var testState testArgState
	afterUnknownFlagWithoutValue := false
	unknownFlagName := ""

	for i := 0; i < len(args); {
		arg := args[i]
		wasAfterUnknown := afterUnknownFlagWithoutValue
		lastUnknownName := unknownFlagName
		afterUnknownFlagWithoutValue = false
		unknownFlagName = ""

		if testState.testArgsFinalized {
			classified = append(classified, ClassifiedArg{
				Index: i,
				Raw:   arg,
				Kind:  ArgTestBinary,
			})
			i++
			continue
		}

		if isTestDelimiter(arg) {
			classified = append(classified, ClassifiedArg{
				Index: i,
				Raw:   arg,
				Kind:  ArgTestDelimiter,
			})
			testState.testArgsFinalized = true
			i++
			continue
		}

		if strings.HasPrefix(arg, "-") {
			if testState.inPkgList {
				testState.inPkgList = false
			}

			consumed, items, unjoinedUnknown, name := classifyTestFlagToken(
				args, i, &testState,
			)
			classified = append(classified, items...)
			afterUnknownFlagWithoutValue = unjoinedUnknown
			unknownFlagName = name
			i += consumed
			continue
		}

		classified = append(classified, classifyPositionalTestArg(
			arg, i, &testState, wasAfterUnknown, lastUnknownName,
		))
		i++
	}

	return classified
}

// classifyArgs scans args and classifies each token according to Go command and subcommand semantics.
// It is non-validating: it preserves tokens as-is and lets cmd/go produce canonical validation errors.
func classifyArgs(subcommand string, args []string) []ClassifiedArg {
	if subcommand == subcmdTest {
		return classifyTestArgs(args)
	}
	return classifyBuildArgs(args)
}
