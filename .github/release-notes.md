### Fixed

- Prevent Oracle password disclosure in `database/sql` span attributes by splitting Easy Connect DSNs on the last unquoted `@` (so passwords containing `@`, `\@`, or quoted characters stay on the credentials side) and safely reading `godror` logfmt and `go-oci8` URL DSNs.

**Full Changelog**: https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/compare/v1.2.1...v1.2.2
