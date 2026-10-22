package crystil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/CrystilAI/go-sdk/internal"
)

// tracingTransport wraps http.RoundTripper to capture LLM calls.
type tracingTransport struct {
	base     http.RoundTripper
	client   *Client
	provider Provider
}

// CloseIdleConnections closes base's idle connections, as http.Client does for
// a transport it holds directly.
func (t *tracingTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// RoundTrip executes the request and captures analytics.
func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Capture request body
	reqBody, err := captureBody(req.Body)
	if err != nil {
		return nil, fmt.Errorf("crystil: capture request: %w", err)
	}
	// A RoundTripper may not change the caller's request: http.Client sends it
	// again on a redirect, with the body changed here and no usage chunk filter.
	req = req.Clone(req.Context())
	var hideUsage bool // the caller did not ask for the usage chunk includeStreamUsage adds
	if t.provider == ProviderOpenAI {
		if body, changed := includeStreamUsage(req.URL.Path, reqBody); changed {
			reqBody = body
			req.ContentLength = int64(len(body))
			req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
			hideUsage = true
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(reqBody))
	query := t.buildQuery(req.URL.Path, reqBody)

	// Check if this is a streaming request. Gemini marks a stream in the URL, not the body.
	isStreaming := isStreamingRequest(query) || strings.HasSuffix(req.URL.Path, ":streamGenerateContent")

	info := t.clientInfo(req.Header)

	// Execute request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// A failed call is not recorded, as the Python SDK records none. Provider SDKs
	// retry some, and each attempt would be recorded as a call with no tokens.
	if resp.StatusCode >= 400 {
		return resp, nil
	}

	// Handle streaming vs non-streaming. Some streams are marked in no way the request
	// shows, such as a multipart form field, but their responses say so.
	if isStreaming || isEventStream(resp) {
		// Wrap response body with streaming handler
		resp.Body = newStreamingResponseBody(
			resp.Body,
			t.client,
			t.provider,
			info,
			query,
			start,
			req.Context(),
		)
		if hideUsage {
			resp.Body = &usageChunkFilter{body: resp.Body}
		}
	} else {
		// Non-streaming: capture complete response
		respBody, err := captureBody(resp.Body)
		if err != nil {
			// The caller reads what it would without Crystil: the bytes, then the error.
			// A failed round trip instead makes the provider SDK send the call again.
			resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(respBody), errorReader{err}))
			return resp, nil
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		// Build and send analytics payload async
		payload := t.buildPayload(req.Context(), query, respBody, info, start, time.Now())
		t.client.collector.SendAsync(payload)
	}

	return resp, nil
}

// geminiURLModel matches the model in a Gemini generate path, as genai builds it:
// ".../models/gemini-2.0-flash:generateContent", or ".../publishers/google/models/..." on Vertex.
// Other calls keep no model, or the backend records a countTokens as a model call.
var geminiURLModel = regexp.MustCompile(`/models/([^/:]+):(generateContent|streamGenerateContent)$`)

// buildQuery parses the request body to record. Gemini names its model only in
// the URL path, so the model is added from there. The bytes sent are not changed.
func (t *tracingTransport) buildQuery(path string, reqBody []byte) map[string]interface{} {
	var query map[string]interface{}
	if len(reqBody) > 0 {
		_ = json.Unmarshal(reqBody, &query)
	}
	if t.provider == ProviderOpenAI && strings.HasSuffix(path, "/chat/completions") {
		addMissingContent(query)
	}
	if t.provider != ProviderGoogle || query["model"] != nil {
		return query
	}

	match := geminiURLModel.FindStringSubmatch(path)
	if match == nil {
		return query
	}
	if query == nil {
		query = map[string]interface{}{}
	}
	query["model"] = match[1]
	return query
}

// addMissingContent gives each chat message sent with no content a null one, as
// the Python SDK records it: openai-go and go-openai leave it out of an assistant
// message with only tool calls, and the backend reads it with no default.
func addMissingContent(query map[string]interface{}) {
	messages, _ := query["messages"].([]interface{})
	for _, m := range messages {
		if message, ok := m.(map[string]interface{}); ok {
			if _, ok := message["content"]; !ok {
				message["content"] = nil
			}
		}
	}
}

// clientInfo names the library that sent the request. A request langchaingo
// sends is recorded as a LangChain call, as the Python SDK records one, so the
// backend routes it the same way.
func (t *tracingTransport) clientInfo(header http.Header) internal.ClientInfo {
	if title := langChainTitle(t.provider); title != "" {
		return internal.ClientInfo{Provider: "langchain", Title: title, Version: langChainVersion()}
	}
	return internal.ClientInfo{Title: string(t.provider), Version: sdkVersion(t.provider, header)}
}

// buildPayload constructs the analytics payload for a call that was not streamed.
func (t *tracingTransport) buildPayload(ctx context.Context, query map[string]interface{}, respBody []byte, info internal.ClientInfo, start, end time.Time) internal.Payload {
	var response map[string]interface{}

	// Parse response body
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &response)
	}

	return t.client.newPayload(ctx, info, query, response, nil, start, end)
}

// newPayload builds the payload for a call, streamed or not, with the
// attribution ctx carries, and its transaction or else the client's. A call
// with exc set failed, and exc says why.
func (c *Client) newPayload(ctx context.Context, info internal.ClientInfo, query map[string]interface{}, response interface{}, exc *string, start, end time.Time) internal.Payload {
	status := "succeeded"
	if exc != nil {
		status = "failed"
	}
	return internal.Payload{
		Attribution: attributionPayload(ctx),
		Conversation: internal.Conversation{
			Client:   info,
			Query:    query,
			Response: response,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: c.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    exc,
				Status: status,
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: c.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: start,
			End:   end,
		},
		Tx: internal.Transaction{
			UUID: c.transaction(ctx),
		},
	}
}

// captureBody reads and returns the body content, and on an error what it read
// before it.
func captureBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	return io.ReadAll(body)
}

// errorReader returns err from every Read.
type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

// includeStreamUsage asks for token usage at the end of a streamed chat
// completion, as the Python SDK does: without stream_options.include_usage,
// OpenAI sends none, and the call is recorded with no tokens.
func includeStreamUsage(path string, body []byte) ([]byte, bool) {
	if !strings.HasSuffix(path, "/chat/completions") {
		return body, false
	}
	var req map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // keeps large numbers, such as a seed, exact
	if err := dec.Decode(&req); err != nil || req["stream"] != true {
		return body, false
	}
	options, _ := req["stream_options"].(map[string]interface{})
	if options["include_usage"] == true {
		return body, false
	}
	if options == nil {
		options = map[string]interface{}{}
	}
	options["include_usage"] = true
	req["stream_options"] = options
	changed, err := json.Marshal(req)
	if err != nil {
		return body, false
	}
	return changed, true
}

// usageChunkFilter hides from the caller the usage chunk includeStreamUsage asked
// for, which the caller did not: it has empty choices, and code that reads
// Choices[0] without checking panics on it. The stream recorded below it keeps it.
type usageChunkFilter struct {
	body    io.ReadCloser
	pending []byte // the start of an event not yet whole
	ready   []byte // whole events for the caller
	err     error  // body's read error, for the caller once ready is empty
}

func (f *usageChunkFilter) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(f.ready) == 0 && f.err == nil {
		n, err := f.body.Read(p)
		f.pending = append(f.pending, p[:n]...)
		f.takeEvents()
		if err != nil {
			f.ready = append(f.ready, f.pending...)
			f.pending = nil
			f.err = err
		}
	}
	n := copy(p, f.ready)
	f.ready = f.ready[n:]
	if len(f.ready) == 0 && f.err != nil {
		return n, f.err
	}
	return n, nil
}

func (f *usageChunkFilter) Close() error {
	return f.body.Close()
}

// takeEvents moves each whole event from pending to ready, leaving out a usage chunk.
func (f *usageChunkFilter) takeEvents() {
	for {
		end := eventEnd(f.pending)
		if end < 0 {
			return
		}
		if event := f.pending[:end]; !isUsageChunk(event) {
			f.ready = append(f.ready, event...)
		}
		f.pending = f.pending[end:]
	}
}

// eventEnd returns the length of the first whole SSE event in b, with the blank
// line that ends it, or -1 if b holds none.
func eventEnd(b []byte) int {
	end := -1
	for _, blank := range [][]byte{[]byte("\n\n"), []byte("\r\n\r\n")} {
		if i := bytes.Index(b, blank); i >= 0 && (end < 0 || i+len(blank) < end) {
			end = i + len(blank)
		}
	}
	return end
}

// isUsageChunk reports whether an SSE event is a chat completion chunk with usage
// and no choices, the one include_usage adds. Other chunks with no choices, such
// as Azure's content filter results, have no usage.
func isUsageChunk(event []byte) bool {
	for _, line := range bytes.Split(event, []byte("\n")) {
		data, ok := bytes.CutPrefix(bytes.TrimRight(line, "\r"), []byte("data:"))
		if !ok {
			continue
		}
		var chunk struct {
			Choices *[]json.RawMessage `json:"choices"`
			Usage   json.RawMessage    `json:"usage"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			return false
		}
		return chunk.Choices != nil && len(*chunk.Choices) == 0 && len(chunk.Usage) > 0 && string(chunk.Usage) != "null"
	}
	return false
}

// isEventStream reports whether resp says it is a stream of server-sent events.
func isEventStream(resp *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return err == nil && mediaType == "text/event-stream"
}

// isStreamingRequest checks if the parsed request body asks for a stream.
func isStreamingRequest(query map[string]interface{}) bool {
	return query["stream"] == true
}
