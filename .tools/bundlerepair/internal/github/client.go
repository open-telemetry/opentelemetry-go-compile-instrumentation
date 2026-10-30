// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package github is a small GitHub REST client. It decodes only the fields
// that the bundle repair commands read.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
)

const (
	// DefaultAPIURL is the GitHub API base URL when GITHUB_API_URL is unset.
	DefaultAPIURL = "https://api.github.com"

	requestTimeout  = 30 * time.Second
	maxBodyBytes    = 32 << 20
	maxErrorBytes   = 1 << 10
	maxPages        = 100
	apiVersion      = "2022-11-28"
	userAgent       = "otelc-bundlerepair"
	acceptMediaType = "application/vnd.github+json"
)

// Client calls the GitHub REST API with one token.
type Client struct {
	base       *url.URL
	token      string
	httpClient *http.Client
}

// New returns a client for the API at baseURL. An empty baseURL selects
// DefaultAPIURL. The token is sent only to the host of baseURL. The URL must
// use https. It may use http only for a loopback host, such as a test server,
// so the token never crosses a network in clear text.
func New(baseURL, token string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultAPIURL
	}
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || base.Host == "" || !secureTransport(base) {
		return nil, errors.New("the GitHub API URL must be an https URL, or an http URL for a loopback host")
	}
	return &Client{
		base:       base,
		token:      token,
		httpClient: &http.Client{Timeout: requestTimeout},
	}, nil
}

// secureTransport reports whether the token may travel to u.
func secureTransport(u *url.URL) bool {
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && isLoopback(u.Hostname())
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// StatusError is a response with a status outside 200 to 299.
type StatusError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
}

// Error describes the failed request. It never includes request headers.
func (e *StatusError) Error() string {
	message := fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		message += ": " + e.Message
	}
	return message
}

// Get requests path and decodes the JSON response into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	body, _, err := c.do(ctx, http.MethodGet, c.endpoint(path, query), nil)
	if err != nil {
		return err
	}
	return errs.Wrap(json.Unmarshal(body, out), "GET "+path+": decode response")
}

// Paginate requests path and follows the rel="next" links. It calls handle once
// for each page body. It stops with an error after 100 pages and when a link
// leaves the API host.
func (c *Client) Paginate(ctx context.Context, path string, query url.Values, handle func(page []byte) error) error {
	next := c.endpoint(path, query)
	for range maxPages {
		body, header, err := c.do(ctx, http.MethodGet, next, nil)
		if err != nil {
			return err
		}
		if err = handle(body); err != nil {
			return err
		}
		link := nextLink(header.Values("Link"))
		if link == "" {
			return nil
		}
		parsed, err := c.base.Parse(link)
		if err != nil {
			return errs.Wrap(err, "GET "+path+": invalid next link")
		}
		if parsed.Scheme != c.base.Scheme || parsed.Host != c.base.Host {
			return errors.New("GET " + path + ": next link leaves the API host")
		}
		next = parsed.String()
	}
	return errors.New("GET " + path + ": more than " + strconv.Itoa(maxPages) + " pages")
}

func (c *Client) endpoint(path string, query url.Values) string {
	target := *c.base
	target.Path = strings.TrimRight(c.base.Path, "/") + "/" + strings.TrimLeft(path, "/")
	target.RawPath = ""
	target.RawQuery = query.Encode()
	return target.String()
}

// GraphQL posts query with variables to the GraphQL endpoint and decodes the
// "data" member of the response into out. A response with errors fails.
func (c *Client) GraphQL(ctx context.Context, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return errs.Wrap(err, "encode GraphQL request")
	}
	body, _, err := c.do(ctx, http.MethodPost, c.graphQLEndpoint(), payload)
	if err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil {
		return errs.Wrap(err, "POST graphql: decode response")
	}
	if len(envelope.Errors) > 0 {
		return errors.New("POST graphql: " + envelope.Errors[0].Message)
	}
	return errs.Wrap(json.Unmarshal(envelope.Data, out), "POST graphql: decode data")
}

// graphQLEndpoint returns the GraphQL URL. GitHub Enterprise Server serves
// REST under /api/v3 and GraphQL under /api/graphql.
func (c *Client) graphQLEndpoint() string {
	target := *c.base
	path := strings.TrimRight(c.base.Path, "/")
	if before, found := strings.CutSuffix(path, "/v3"); found {
		path = before
	}
	target.Path = path + "/graphql"
	target.RawPath = ""
	target.RawQuery = ""
	return target.String()
}

func (c *Client) do(ctx context.Context, method, target string, payload []byte) ([]byte, http.Header, error) {
	if c.token == "" {
		return nil, nil, errors.New("GH_TOKEN must be set")
	}
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, nil, errs.Wrap(err, "build request")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", acceptMediaType)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Github-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// The url.Error text holds the URL only. It never holds the token.
		return nil, nil, errs.Wrap(err, method+" "+req.URL.Path)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, nil, statusError(req, resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, nil, errs.Wrap(err, method+" "+req.URL.Path+": read response")
	}
	if len(body) > maxBodyBytes {
		return nil, nil, errors.New(
			method + " " + req.URL.Path + ": response is larger than " + strconv.Itoa(maxBodyBytes) + " bytes",
		)
	}
	return body, resp.Header, nil
}

func statusError(req *http.Request, resp *http.Response) error {
	statusErr := &StatusError{Method: req.Method, Path: req.URL.Path, StatusCode: resp.StatusCode}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	if err == nil {
		var payload struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &payload) == nil {
			statusErr.Message = payload.Message
		}
	}
	return statusErr
}

// nextLink returns the rel="next" target of Link header values, or "".
func nextLink(values []string) string {
	for _, value := range values {
		for rest := strings.TrimSpace(value); strings.HasPrefix(rest, "<"); {
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				break
			}
			target := rest[1:end]
			params, remaining, _ := strings.Cut(rest[end+1:], ",")
			if hasRelNext(params) {
				return target
			}
			rest = strings.TrimSpace(remaining)
		}
	}
	return ""
}

func hasRelNext(params string) bool {
	for param := range strings.SplitSeq(params, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(param), "=")
		if !found || !strings.EqualFold(name, "rel") {
			continue
		}
		for rel := range strings.FieldsSeq(strings.Trim(value, `",`)) {
			if strings.EqualFold(rel, "next") {
				return true
			}
		}
	}
	return false
}
