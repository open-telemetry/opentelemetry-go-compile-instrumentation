# Hook context overhead

This standalone program isolates trampoline overhead from SDK work. Each target
is noinline, has zero, three or six integer parameters and returns one integer.
Before, After and paired hooks have empty bodies. A global sink retains results.
`main_test.go` checks the target results. The module uses the existing local hook
API and adds no external dependencies.

From this directory (use absolute paths to each otelc binary):

```sh
go test ./...
go build -o plain .
/path/to/main/otelc --rules rules.yaml go build -a -o before .
/path/to/changed/otelc --rules rules.yaml go build -a -gcflags=-m -o after .
for i in $(seq 1 8); do ./plain -test.benchtime=100ms; done > plain.txt
for i in $(seq 1 8); do ./before -test.benchtime=100ms; done > before.txt
for i in $(seq 1 8); do ./after -test.benchtime=100ms; done > after.txt
benchstat plain.txt before.txt after.txt
benchstat before.txt after.txt
```

Use `-a` when switching otelc binaries to avoid reusing instrumented build-cache
entries. To compare escape diagnostics, build both instrumented binaries with
`-a -gcflags=-m`, capturing stderr. Ignore unrelated startup/runtime escapes;
compare the targets' heap moves and trampoline context/slice escapes.
