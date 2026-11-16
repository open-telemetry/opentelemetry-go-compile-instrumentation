### Fixed

- Prevent MySQL password disclosure in `database/sql` span attributes by matching the driver's DSN credential boundary rules and skipping the generic `ParseDbName` fallback for drivers with a registered parser.
- Prevent path traversal and escaping symlink reads in file instrumentation rules (`InstFileRule`) by rejecting absolute, root/volume-relative, or `..` paths and confining reads to the rule package directory with `os.OpenRoot`.

**Full Changelog**: https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/compare/v1.2.0...v1.2.1
