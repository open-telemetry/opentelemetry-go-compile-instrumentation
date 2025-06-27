# build instrumentation tool
go mod tidy
go build -a -o otel ./tool/cmd
TOOL=$(pwd)/otel
# compile-time instrumentation via toolexec
cd demo
go build -a -work -toolexec=$TOOL -o demo .
./demo