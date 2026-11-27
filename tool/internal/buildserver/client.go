// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package buildserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// ErrUnavailable is returned when the build server is not configured or cannot
// be reached, signaling callers to fall back to the file-based path.
var ErrUnavailable = ex.New("build server unavailable")

const (
	dialTimeout       = 2 * time.Second
	metadataTimeout   = 5 * time.Second
	maxErrorBodyBytes = 4096
)

// Client talks to an in-memory build Server over UDS or loopback TCP.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// ClientFromEnv returns a Client for OTELC_BUILD_SERVER, or nil when unset,
// malformed, or bound to a different invocation directory.
func ClientFromEnv() *Client {
	addr := os.Getenv(util.EnvOtelcBuildServer)
	if addr == "" {
		return nil
	}
	if dir := os.Getenv(util.EnvOtelcBuildServerDir); dir != "" &&
		filepath.Clean(dir) != filepath.Clean(util.GetInvocationDir()) {
		return nil
	}
	c, err := NewClient(addr, os.Getenv(util.EnvOtelcBuildServerAuth))
	if err != nil {
		return nil
	}
	return c
}

// NewClient constructs a Client for addr ("unix:<path>" or "tcp:<host:port>")
// and optional bearer token.
func NewClient(addr, token string) (*Client, error) {
	dialer := &net.Dialer{Timeout: dialTimeout}

	var baseURL string
	var transport *http.Transport
	switch {
	case strings.HasPrefix(addr, "unix:"):
		sockPath := strings.TrimPrefix(addr, "unix:")
		if sockPath == "" {
			return nil, ex.Wrapf(ErrUnavailable, "empty unix socket path")
		}
		baseURL = "http://otelc"
		transport = &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", sockPath)
			},
		}
	case strings.HasPrefix(addr, "tcp:"):
		hostPort := strings.TrimPrefix(addr, "tcp:")
		if hostPort == "" {
			return nil, ex.Wrapf(ErrUnavailable, "empty tcp address")
		}
		baseURL = "http://" + hostPort
		transport = &http.Transport{
			Proxy:       nil,
			DialContext: dialer.DialContext,
		}
	default:
		return nil, ex.Wrapf(ErrUnavailable, "unsupported address %q", addr)
	}

	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Transport: transport},
	}, nil
}

// ToolID fetches the cached rules digest from GET /v1/tool-id.
func (c *Client) ToolID(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	var resp ToolIDResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tool-id", nil, &resp); err != nil {
		return "", err
	}
	return resp.ToolID, nil
}

// CompileLookup fetches rule sets and build metadata for pkg from
// GET /v1/compile?pkg=<pkg>.
func (c *Client) CompileLookup(ctx context.Context, pkg string) (*CompileResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	var resp CompileResponse
	path := "/v1/compile?pkg=" + url.QueryEscape(pkg)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ResolveExports requests coalesced export resolution from
// POST /v1/resolve-export.
func (c *Client) ResolveExports(ctx context.Context, req ResolveExportRequest) (*ResolveExportResponse, error) {
	var resp ResolveExportResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/resolve-export", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, reqBody, out any) error {
	var body io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ex.Join(ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return ex.Newf(
			"build server %s %s: status %d: %s",
			method, path, resp.StatusCode, strings.TrimSpace(string(msg)),
		)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
