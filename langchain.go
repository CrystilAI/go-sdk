// LangChain integration for Payloop analytics.
package payloop

import (
	"context"
	"time"

	"github.com/PayloopAI/go-sdk/internal"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/schema"
)

// langChainHandler is a LangChain callback handler that tracks LLM calls with Payloop.
type langChainHandler struct {
	collector   *collector
	cfg         *config
	txID        string
	attribution *Attribution
	startTime   map[string]time.Time
	sendFn      func(context.Context, internal.Payload)
}

// newLangChainHandler creates a new Payloop callback handler for LangChain.
func newLangChainHandler(collector *collector, cfg *config, txID string, attr *Attribution) *langChainHandler {
	h := &langChainHandler{
		collector:   collector,
		cfg:         cfg,
		txID:        txID,
		attribution: attr,
		startTime:   make(map[string]time.Time),
	}
	// Default sendFn to use the collector
	h.sendFn = collector.SendAsync
	return h
}

// HandleLLMStart is called when an LLM starts processing.
func (h *langChainHandler) HandleLLMStart(ctx context.Context, prompts []string) {
	h.startTime[h.txID] = time.Now()
}

// HandleLLMGenerateContentStart is called when content generation starts.
func (h *langChainHandler) HandleLLMGenerateContentStart(ctx context.Context, ms []llms.MessageContent) {
	h.startTime[h.txID] = time.Now()
}

// HandleLLMEnd is called when an LLM finishes processing.
func (h *langChainHandler) HandleLLMEnd(ctx context.Context, result map[string]any) {
	endTime := time.Now()
	startTime := h.startTime[h.txID]
	if startTime.IsZero() {
		startTime = endTime // Fallback if start wasn't tracked
	}
	delete(h.startTime, h.txID)

	// Build query and response from result map
	query := make(map[string]interface{})
	response := make(map[string]interface{})

	// Copy result data to response
	for k, v := range result {
		response[k] = v
	}

	// Build attribution for payload
	var attr interface{}
	if h.attribution != nil {
		attr = map[string]interface{}{
			"parent": map[string]interface{}{
				"id":   h.attribution.Parent.ID,
				"name": h.attribution.Parent.Name,
			},
		}
		if h.attribution.Subsidiary != nil {
			attr.(map[string]interface{})["subsidiary"] = map[string]interface{}{
				"id":   h.attribution.Subsidiary.ID,
				"name": h.attribution.Subsidiary.Name,
			}
		}
	}

	// Build and send analytics payload
	payload := internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: "",
				Title:    "langchain",
				Version:  "",
			},
			Query:    query,
			Response: response,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: h.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    nil,
				Status: "succeeded",
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: h.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: startTime,
			End:   endTime,
		},
		Tx: internal.Transaction{
			UUID: h.txID,
		},
	}

	h.sendFn(ctx, payload)
}

// HandleLLMGenerateContentEnd is called when content generation finishes.
func (h *langChainHandler) HandleLLMGenerateContentEnd(ctx context.Context, response *llms.ContentResponse) {
	endTime := time.Now()
	startTime := h.startTime[h.txID]
	if startTime.IsZero() {
		startTime = endTime
	}
	delete(h.startTime, h.txID)

	// Build query from content response
	query := make(map[string]interface{})
	query["messages"] = []map[string]interface{}{
		{
			"role":    "user",
			"content": "", // Input prompts not available in this callback
		},
	}

	// Build response from content response
	responseMap := make(map[string]interface{})
	if response != nil && len(response.Choices) > 0 {
		choices := make([]map[string]interface{}, 0)
		for _, choice := range response.Choices {
			choices = append(choices, map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": choice.Content,
				},
			})
		}
		responseMap["choices"] = choices
	}

	// Build attribution for payload
	var attr interface{}
	if h.attribution != nil {
		attr = map[string]interface{}{
			"parent": map[string]interface{}{
				"id":   h.attribution.Parent.ID,
				"name": h.attribution.Parent.Name,
			},
		}
		if h.attribution.Subsidiary != nil {
			attr.(map[string]interface{})["subsidiary"] = map[string]interface{}{
				"id":   h.attribution.Subsidiary.ID,
				"name": h.attribution.Subsidiary.Name,
			}
		}
	}

	// Build and send analytics payload
	payload := internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: "",
				Title:    "langchain",
				Version:  "",
			},
			Query:    query,
			Response: responseMap,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: h.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    nil,
				Status: "succeeded",
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: h.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: startTime,
			End:   endTime,
		},
		Tx: internal.Transaction{
			UUID: h.txID,
		},
	}

	h.sendFn(ctx, payload)
}

// HandleLLMError is called when an LLM encounters an error.
func (h *langChainHandler) HandleLLMError(ctx context.Context, err error) {
	endTime := time.Now()
	startTime := h.startTime[h.txID]
	if startTime.IsZero() {
		startTime = endTime
	}
	delete(h.startTime, h.txID)

	errMsg := err.Error()

	// Build attribution for payload
	var attr interface{}
	if h.attribution != nil {
		attr = map[string]interface{}{
			"parent": map[string]interface{}{
				"id":   h.attribution.Parent.ID,
				"name": h.attribution.Parent.Name,
			},
		}
		if h.attribution.Subsidiary != nil {
			attr.(map[string]interface{})["subsidiary"] = map[string]interface{}{
				"id":   h.attribution.Subsidiary.ID,
				"name": h.attribution.Subsidiary.Name,
			}
		}
	}

	// Build and send error payload
	payload := internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: "",
				Title:    "langchain",
				Version:  "",
			},
			Query:    make(map[string]interface{}),
			Response: make(map[string]interface{}),
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: h.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    &errMsg,
				Status: "failed",
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: h.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: startTime,
			End:   endTime,
		},
		Tx: internal.Transaction{
			UUID: h.txID,
		},
	}

	h.sendFn(ctx, payload)
}

// HandleChainStart is called when a chain starts processing.
func (h *langChainHandler) HandleChainStart(ctx context.Context, inputs map[string]any) {
	// Optional: track chain execution
}

// HandleChainEnd is called when a chain finishes processing.
func (h *langChainHandler) HandleChainEnd(ctx context.Context, outputs map[string]any) {
	// Optional: track chain execution
}

// HandleChainError is called when a chain encounters an error.
func (h *langChainHandler) HandleChainError(ctx context.Context, err error) {
	// Optional: track chain errors
}

// HandleToolStart is called when a tool starts executing.
func (h *langChainHandler) HandleToolStart(ctx context.Context, input string) {
	// Optional: track tool execution
}

// HandleToolEnd is called when a tool finishes executing.
func (h *langChainHandler) HandleToolEnd(ctx context.Context, output string) {
	// Optional: track tool execution
}

// HandleToolError is called when a tool encounters an error.
func (h *langChainHandler) HandleToolError(ctx context.Context, err error) {
	// Optional: track tool errors
}

// HandleText is called with arbitrary text events.
func (h *langChainHandler) HandleText(ctx context.Context, text string) {
	// Optional: handle text events
}

// HandleAgentAction is called when an agent takes an action.
func (h *langChainHandler) HandleAgentAction(ctx context.Context, action schema.AgentAction) {
	// Optional: track agent actions
}

// HandleAgentFinish is called when an agent finishes.
func (h *langChainHandler) HandleAgentFinish(ctx context.Context, finish schema.AgentFinish) {
	// Optional: track agent finish
}

// HandleRetrieverStart is called when a retriever starts.
func (h *langChainHandler) HandleRetrieverStart(ctx context.Context, query string) {
	// Optional: track retriever execution
}

// HandleRetrieverEnd is called when a retriever ends.
func (h *langChainHandler) HandleRetrieverEnd(ctx context.Context, query string, documents []schema.Document) {
	// Optional: track retriever execution
}

// HandleStreamingFunc is called for streaming responses.
func (h *langChainHandler) HandleStreamingFunc(ctx context.Context, chunk []byte) {
	// Optional: handle streaming chunks
}

// Ensure langChainHandler implements callbacks.Handler interface
var _ callbacks.Handler = (*langChainHandler)(nil)
