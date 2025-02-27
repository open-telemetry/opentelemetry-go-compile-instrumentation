# build instrumentation tool
go build -a -o otel
TOOL=$(pwd)/otel
# compile-time instrumentation via toolexec
cd demo
rm -rf save.go
go build -a -toolexec=$TOOL -o demo .
./demo