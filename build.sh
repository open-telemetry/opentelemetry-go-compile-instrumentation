# build instrumentation tool
go build -a -o otel
TOOL=$(pwd)/otel
# compile-time instrumentation via toolexec
cd demo
# TODO
# The 'tidy' step should ideally be invoked by the tool as the first stage.
# However, since this is not implemented yet, it must be run separately here
# as a standalone command to resolve all dependencies.
go mod tidy
go build -a -work -toolexec=$TOOL -o demo .
./demo
