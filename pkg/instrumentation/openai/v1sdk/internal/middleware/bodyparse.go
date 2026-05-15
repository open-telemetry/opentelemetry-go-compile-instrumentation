// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const (
	// maxRequestBodyBytes caps request body buffering. OpenAI chat requests
	// with very large message histories stay well under this limit; anything
	// larger is likely a multipart upload we don't inspect anyway.
	maxRequestBodyBytes = 1 << 20 // 1 MiB
	// maxResponseBodyBytes caps response body buffering. Non-streaming chat
	// completion responses are small JSON documents.
	maxResponseBodyBytes = 4 << 20 // 4 MiB
)

// chatRequest is the minimal subset of a chat completion request body that
// the middleware needs to extract attributes. Kept deliberately small so we
// don't tie the middleware to any specific openai-go type.
type chatRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// chatResponse is the minimal subset of a chat completion response body that
// the middleware needs to set span attributes and record token metrics.
type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

// finishReasons flattens Choices[].FinishReason into a slice for the
// gen_ai.response.finish_reasons attribute.
func (r *chatResponse) finishReasons() []string {
	out := make([]string, 0, len(r.Choices))
	for _, c := range r.Choices {
		out = append(out, c.FinishReason)
	}
	return out
}

// bufferAndParseRequest reads the full request body (bounded), attempts to
// decode it as a chat request, and returns both the raw bytes and the parsed
// fields. The raw bytes MUST be used to restore req.Body before the request
// is sent downstream, otherwise the SDK would see an empty body.
//
// Parse failures are non-fatal: the middleware still creates a span with
// whatever attributes it does have. This is important because not every
// OpenAI endpoint uses JSON bodies (image uploads use multipart, etc.).
func bufferAndParseRequest(req *http.Request) (buf []byte, parsed *chatRequest) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	buf, err := io.ReadAll(io.LimitReader(req.Body, maxRequestBodyBytes))
	_ = req.Body.Close()
	if err != nil || len(buf) == 0 {
		return buf, nil
	}
	if !looksLikeJSON(buf) {
		return buf, nil
	}
	var r chatRequest
	if err := json.Unmarshal(buf, &r); err != nil {
		return buf, nil
	}
	return buf, &r
}

// bufferAndParseResponse does the same for a response body. The caller MUST
// restore resp.Body with the returned bytes so the SDK can decode the
// response into its typed result.
func bufferAndParseResponse(resp *http.Response) (buf []byte, parsed *chatResponse) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	_ = resp.Body.Close()
	if err != nil || len(buf) == 0 {
		return buf, nil
	}
	if !looksLikeJSON(buf) {
		return buf, nil
	}
	var r chatResponse
	if err := json.Unmarshal(buf, &r); err != nil {
		return buf, nil
	}
	return buf, &r
}

// looksLikeJSON is a cheap pre-check so we don't invoke json.Unmarshal on
// binary or multipart payloads. A real JSON object/array always starts with
// '{' or '[' after leading whitespace.
func looksLikeJSON(buf []byte) bool {
	trimmed := bytes.TrimLeft(buf, " \t\r\n")
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}

// isStreamingResponse returns true when the response is a Server-Sent Events
// stream, in which case the middleware MUST NOT buffer the body or it will
// break the user's streaming iterator and buffer an unbounded amount of data.
func isStreamingResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
}

// restoreBody replaces body with an in-memory reader over buf so the next
// consumer can still read it end-to-end.
func restoreBody(buf []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(buf))
}
