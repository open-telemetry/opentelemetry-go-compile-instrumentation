package setup

import (
	"path/filepath"
	"strings"
	"testing"
)

// Regression (#1207): cmd/go splits the -toolexec value on whitespace
// (internal/quoted), so an otelc executable installed under a path with
// spaces — "Program Files" being the obvious one — must be quoted or `go`
// runs only the first chunk. The constructed flag value must therefore
// contain the whole executable path as a single whitespace-free token after
// go's unquoting, and the " toolexec" argument must survive as its own token.
func TestToolexecFlagQuotesPathWithSpaces(t *testing.T) {
	execPath := filepath.Join("C:", "Program Files", "otelc", "otelc.exe")
	insert := "-toolexec=" + quoteForGo(execPath) + " toolexec"

	value := strings.TrimPrefix(insert, "-toolexec=")

	// go's internal/quoted split: honor double quotes, split on whitespace.
	tokens := splitQuoted(value)
	if len(tokens) != 2 {
		t.Fatalf("expected [exec arg] tokens after go's split, got %d: %q", len(tokens), tokens)
	}
	if tokens[0] != execPath {
		t.Fatalf("executable token = %q, want the full path %q", tokens[0], execPath)
	}
	if tokens[1] != "toolexec" {
		t.Fatalf("argument token = %q, want %q", tokens[1], "toolexec")
	}

	// Without quoting the split drops everything after the first space.
	unquoted := "-toolexec=" + execPath + " toolexec"
	bare := strings.Fields(strings.TrimPrefix(unquoted, "-toolexec="))
	if bare[0] != "C:Program" || len(bare) > 3 {
		t.Fatalf("unquoted construction should break at the space, got %q", bare)
	}
}

// splitQuoted mimics cmd/go's internal/quoted splitting closely enough for
// this test: double-quoted runs count as one token, else split on spaces.
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
