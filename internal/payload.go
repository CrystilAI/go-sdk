package internal

import (
	"encoding/json"
	"time"
)

// Payload represents analytics data sent to Crystil.
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
	Response interface{}            `json:"response"` // an object, or for some streams the list of events
}

// ClientInfo describes the LLM provider client.
type ClientInfo struct {
	Provider string
	Title    string
	Version  string
}

// MarshalJSON sends an empty Version as null, as the other SDKs do.
func (c ClientInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Provider string  `json:"provider,omitempty"`
		Title    string  `json:"title"`
		Version  *string `json:"version"`
	}{c.Provider, c.Title, nullIfEmpty(c.Version)})
}

// nullIfEmpty returns nil for "", which JSON sends as null.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
	Start time.Time
	End   time.Time
}

// MarshalJSON sends the times as Unix seconds, as the other SDKs do: the
// backend reads them with fromtimestamp, which rejects a string.
func (t TimeInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	}{unixSeconds(t.Start), unixSeconds(t.End)})
}

// unixSeconds returns t in Unix seconds, to the microsecond like Python's time.time().
func unixSeconds(t time.Time) float64 {
	return float64(t.UnixMicro()) / 1e6
}

// Transaction contains transaction UUID.
type Transaction struct {
	UUID string `json:"uuid"`
}
