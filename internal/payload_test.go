package internal

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

// TestTimeInfoSendsUnixSeconds checks that time.start and time.end go out as
// Unix seconds, as the other SDKs send them: the backend reads them with
// fromtimestamp and quarantines a payload whose times are strings.
func TestTimeInfoSendsUnixSeconds(t *testing.T) {
	// The times of a Python payload in the backend's fixtures.
	data, err := json.Marshal(Payload{Time: TimeInfo{
		Start: time.Unix(1778892773, 878926000),
		End:   time.Unix(1778892775, 881186000),
	}})
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Time struct{ Start, End any }
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		got  any
		want string
	}{
		"start": {got.Time.Start, "1778892773.878926"},
		"end":   {got.Time.End, "1778892775.881186"},
	} {
		if n, ok := tc.got.(json.Number); !ok || n.String() != tc.want {
			t.Errorf("time.%s = %#v, want the number %s", name, tc.got, tc.want)
		}
	}
}

// TestClientInfoSendsNullVersion checks that a payload whose provider SDK version
// is unknown sends conversation.client.version as null, as the other SDKs do.
func TestClientInfoSendsNullVersion(t *testing.T) {
	data, err := json.Marshal(ClientInfo{Title: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if version, ok := got["version"]; !ok || version != nil {
		t.Errorf("conversation.client = %s, want version null", data)
	}
}

// TestPayloadSendsNullAttribution checks that a payload with no attribution sends
// attribution as null, as the other SDKs do: the backend quarantines a payload
// without the key.
func TestPayloadSendsNullAttribution(t *testing.T) {
	data, err := json.Marshal(Payload{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if attribution, ok := got["attribution"]; !ok || attribution != nil {
		t.Errorf("payload = %s, want attribution null", data)
	}
}

// TestClientInfoSendsNullProvider checks that a payload with no framework sends
// conversation.client.provider as null, as the other SDKs do, not without the key.
func TestClientInfoSendsNullProvider(t *testing.T) {
	data, err := json.Marshal(ClientInfo{Title: "openai", Version: "3.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if provider, ok := got["provider"]; !ok || provider != nil {
		t.Errorf("conversation.client = %s, want provider null", data)
	}
}
