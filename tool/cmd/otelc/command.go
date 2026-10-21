// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/setup"
	"go.opentelemetry.io/otelc/tool/util"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandCleanup = cli.Command{
	Name:        "cleanup",
	Description: "Remove all artifacts created by the setup and build phases",
	Before:      addLoggerPhaseAttribute,
	Action: func(ctx context.Context, _ *cli.Command) error {
		return setup.Cleanup(ctx, true)
	},
}

//nolint:gochecknoglobals // Implementation of a CLI command
var commandGo = cli.Command{
	Name:            "go",
	Description:     "Invoke the go toolchain with toolexec mode",
	ArgsUsage:       "[go toolchain flags]",
	SkipFlagParsing: true,
	Before:          addLoggerPhaseAttribute,
	Action:          setup.GoBuild,
}

//nolint:gochecknoglobals // Implementation of a CLI command
var commandPin = cli.Command{
	Name:        "pin",
	Description: "Generate or update otel.instrumentation.go to pin instrumentation packages for the current module.",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "prune",
			Usage: "Prune invalid imports within otel.instrumentation.go",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "validate",
			Usage: "Validate that all imports in otel.instrumentation.go contain valid rules",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "generate",
			Usage: "Manages //go:generate directive in otel.instrumentation.go",
		},
	},
	Before: addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		opts := setup.PinOptions{
			Prune:    cmd.Bool("prune"),
			Generate: nil,
			Validate: cmd.Bool("validate"),
			Args:     cmd.Args().Slice(),
		}
		if cmd.IsSet("generate") {
			generate := cmd.Bool("generate")
			opts.Generate = &generate
		}

		_, err := setup.Pin(ctx, opts)
		return err
	},
}

//nolint:gochecknoglobals // Implementation of a CLI command
var commandVendor = cli.Command{
	Name: "vendor",
	Description: "Add the modules of the instrumentation hook code to vendor/ of the current module. " +
		"After this, otelc go build needs no network and no module cache. " +
		"When the hook code needs a higher version of a vendored module, otelc vendor updates it " +
		"(like go get) and prints the change. " +
		"The go.mod file then has replace directives to ./.otelc-build, which is not committed: " +
		"go build and otelc go build work without it, but go mod tidy, go mod vendor and " +
		"go mod verify need it, so run otelc vendor again first (for example in a new checkout). " +
		"This stays until the instrumentation modules are published " +
		"(https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/585). " +
		"The module needs go 1.14 or higher in go.mod.",
	ArgsUsage:       "[go build flags] [packages]",
	SkipFlagParsing: true,
	Before:          addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		args := cmd.Args().Slice()
		// SkipFlagParsing passes all the arguments to Vendor, so the help
		// flags are handled here.
		if len(args) == 1 && isHelpFlag(args[0]) {
			return cli.ShowSubcommandHelp(cmd)
		}
		return setup.Vendor(ctx, args)
	},
}

//nolint:gochecknoglobals // Implementation of a CLI command
var commandSetup = cli.Command{
	Name:        "setup",
	Description: "Set up the environment for instrumentation",
	Before:      addLoggerPhaseAttribute,
	Action:      setup.Setup,
}

//nolint:gochecknoglobals // Implementation of a CLI command
var commandToolexec = cli.Command{
	Name:            "toolexec",
	Description:     "Wrap a command run by the go toolchain",
	SkipFlagParsing: true,
	Hidden:          true,
	Before:          addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		nested := os.Getenv(util.EnvOtelcNestedToolexec) != ""
		if !nested {
			// Here, not in instrument.Toolexec, so os.Executable resolves to
			// this binary for the go commands it spawns.
			if err := instrument.EnableNestedToolexec(); err != nil {
				return err
			}
		}
		return instrument.Toolexec(ctx, cmd.Args().Slice(), nested)
	},
}

//nolint:gochecknoglobals // Implementation of a CLI command
var commandVersion = cli.Command{
	Name:        "version",
	Description: "Print the version of the tool",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "verbose",
			Usage: "Print additional information about the tool",
		},
	},
	Before: addLoggerPhaseAttribute,
	Action: func(_ context.Context, cmd *cli.Command) error {
		_, err := fmt.Fprintf(cmd.Writer, "otelc version %s", util.Version)
		if err != nil {
			return ex.Wrapf(err, "failed to print version")
		}

		if util.CommitHash != "unknown" {
			_, err = fmt.Fprintf(cmd.Writer, "+%s", util.CommitHash)
			if err != nil {
				return ex.Wrapf(err, "failed to print commit hash")
			}
		}

		if util.BuildTime != "unknown" {
			_, err = fmt.Fprintf(cmd.Writer, " (%s)", util.BuildTime)
			if err != nil {
				return ex.Wrapf(err, "failed to print build time")
			}
		}

		_, err = fmt.Fprint(cmd.Writer, "\n")
		if err != nil {
			return ex.Wrapf(err, "failed to print newline")
		}

		if cmd.Bool("verbose") {
			_, err = fmt.Fprintf(cmd.Writer, "%s\n", runtime.Version())
			if err != nil {
				return ex.Wrapf(err, "failed to print runtime version")
			}
		}

		return nil
	},
}

// isHelpFlag reports whether arg asks for the help of a command.
func isHelpFlag(arg string) bool {
	switch arg {
	case "-h", "--h", "-help", "--help":
		return true
	default:
		return false
	}
}
