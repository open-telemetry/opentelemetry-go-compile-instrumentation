# Built-in Profiling

`otelc` has built-in support for collecting pprof CPU, heap, and execution trace
profiles during a build. This lets you measure and compare the overhead of
compile-time instrumentation across different runs.

## Collecting profiles

Use the hidden `--profile-path` and `--profile` flags:

```bash
otelc --profile-path="$PWD/profiles" --profile=cpu go build ./...
```

Or via environment variables:

```bash
OTELC_PROFILE_PATH="$PWD/profiles" OTELC_ENABLED_PROFILES=cpu,heap otelc go build ./...
```

The `--profile` flag can be repeated and accepts:

| Value   | Description                                             |
|---------|---------------------------------------------------------|
| `cpu`   | CPU time profile (sampled at 100 Hz)                    |
| `heap`  | Heap allocation profile (snapshot at build completion)  |
| `trace` | Execution trace (per-process, cannot be merged)         |

### How profile files are named

`otelc go build` spawns many sub-processes — one `otelc toolexec` invocation
per package compiled by the Go toolchain. Each process writes its own
PID-stamped files to avoid collisions:

```
profiles/
  otelc-cpu-12345.pprof    ← parent process (setup + build orchestration)
  otelc-cpu-12400.pprof    ← toolexec process for net/http
  otelc-cpu-12401.pprof    ← toolexec process for encoding/json
  ...
  otelc-heap-12345.pprof
  otelc-heap-12400.pprof
  ...
```

## Analyzing profiles

Merge all CPU profiles for a complete picture across all processes:

```bash
go tool pprof -proto profiles/otelc-cpu-*.pprof > profiles/cpu.pprof
```

Open an interactive web UI:

```bash
go tool pprof -http=:8080 profiles/cpu.pprof
```

Show the top 20 functions by CPU time:

```bash
go tool pprof -top profiles/cpu.pprof
```

For execution traces (per-process, cannot be merged), view each file individually:

```bash
go tool trace profiles/otelc-12345.trace
```

## Summary mode

Use `--profile-summary` to automatically merge all PID-stamped files into a
single file per type after the build completes. Individual files are removed.

```bash
otelc --profile-path="$PWD/profiles" --profile=cpu --profile=heap \
      --profile-summary go build ./...
```

After the build:

```
profiles/
  otelc-cpu.pprof    ← merged CPU profile (all processes)
  otelc-heap.pprof   ← merged heap profile (all processes)
```

This is useful for a quick high-level view without managing hundreds of files.

> Note: Execution traces are not affected by `--profile-summary` because the
> Go trace tool does not support merging multiple trace files.

## Comparing two builds

To measure the effect of a change, collect a baseline and a modified profile,
then compare them using `go tool pprof -diff_base`:

```bash
# Baseline run
otelc --profile-path="$PWD/baseline" --profile=cpu --profile-summary go build ./...

# Modified run (e.g., after a code change)
otelc --profile-path="$PWD/modified" --profile=cpu --profile-summary go build ./...

# Compare: positive values = more time in modified, negative = less
go tool pprof -http=:8080 -diff_base=baseline/otelc-cpu.pprof modified/otelc-cpu.pprof
```

The flame graph diff highlights regressions (red) and improvements (green).
