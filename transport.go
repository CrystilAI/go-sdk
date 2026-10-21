package crystil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
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

	// Check if this is a streaming request. Gemini marks a stream in the URL, not the body.
	isStreaming := isStreamingRequest(reqBody) || strings.HasSuffix(req.URL.Path, ":streamGenerateContent")

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
			reqBody,
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
		payload := t.buildPayload(reqBody, respBody, start, time.Now())
		t.client.collector.SendAsync(payload)
	}

	return resp, nil
}

// buildPayload constructs the analytics payload.
func (t *tracingTransport) buildPayload(reqBody, respBody []byte, start, end time.Time) internal.Payload {
	var query map[string]interface{}
	var response map[string]interface{}

	// Parse request and response bodies
	if len(reqBody) > 0 {
		_ = json.Unmarshal(reqBody, &query)
	}
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &response)
	}

	// Build attribution for payload (read from client dynamically)
	var attr interface{}
	if t.client.attribution != nil {
		parentMap := map[string]interface{}{
			"id": t.client.attribution.ParentID,
		}
		if t.client.attribution.ParentName != nil {
			parentMap["name"] = *t.client.attribution.ParentName
		}

		attr = map[string]interface{}{
			"parent": parentMap,
		}

		if t.client.attribution.SubsidiaryID != nil {
			subsidiaryMap := map[string]interface{}{
				"id": *t.client.attribution.SubsidiaryID,
			}
			if t.client.attribution.SubsidiaryName != nil {
				subsidiaryMap["name"] = *t.client.attribution.SubsidiaryName
			}
			attr.(map[string]interface{})["subsidiary"] = subsidiaryMap
		}
	}

	return internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: "",
				Title:    string(t.provider),
				Version:  "",
			},
			Query:    query,
			Response: response,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: t.client.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    nil,
				Status: "succeeded",
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: t.client.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: start,
			End:   end,
		},
		Tx: internal.Transaction{
			UUID: t.client.txID,
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

// isStreamingRequest checks if the request body indicates streaming.
func isStreamingRequest(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}

	// Check for "stream": true
	if stream, ok := req["stream"].(bool); ok && stream {
		return true
	}

	return false
}
