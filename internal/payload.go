package internal

import "time"

// Payload represents analytics data sent to Payloop.
type Payload struct {
	Attribution  interface{}  `json:"attribution,omitempty"`
	Conversation Conversation `json:"conversation"`
	Meta         Meta         `json:"meta"`
	Time         TimeInfo     `json:"time"`
	Tx           Transaction  `json:"tx"`
}

// Conversation contains request and response data.
type Conversation struct {
	Client   ClientInfo             `json:"client"`
	Query    map[string]interface{} `json:"query"`
	Response map[string]interface{} `json:"response"`
}

// ClientInfo describes the LLM provider client.
type ClientInfo struct {
	Provider string `json:"provider,omitempty"`
	Title    string `json:"title"`
	Version  string `json:"version,omitempty"`
}

// Meta contains metadata about the request.
type Meta struct {
	API  APIInfo  `json:"api"`
	FNFG FNFGInfo `json:"fnfg"`
	SDK  SDKInfo  `json:"sdk"`
}

// APIInfo contains API key information.
type APIInfo struct {
	Key string `json:"key"`
}

// FNFGInfo contains fire-and-forget status.
type FNFGInfo struct {
	Exc    *string `json:"exc"`
	Status string  `json:"status"`
}

// SDKInfo describes the SDK being used.
type SDKInfo struct {
	Client  string `json:"client"`
	Version string `json:"version"`
}

// TimeInfo contains timing information.
type TimeInfo struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Transaction contains transaction UUID.
type Transaction struct {
	UUID string `json:"uuid"`
}
