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
