package crystil

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSetAttribution(t *testing.T) {
	johnDoe := "John Doe"
	acmeCorp := "Acme Corp"
	engineering := "Engineering"
	teamID := "team-456"
	// The caps count characters, as Python's do, not bytes: each of these is 3 or 4.
	hundredChars := strings.Repeat("中", 100)
	tooManyChars := strings.Repeat("中", 101)

	tests := []struct {
		name    string
		attr    *Attribution
		wantErr bool
	}{
		{
			name: "valid parent only",
			attr: &Attribution{
				ParentID:   "user-123",
				ParentName: &johnDoe,
			},
			wantErr: false,
		},
		{
			name: "valid parent and subsidiary",
			attr: &Attribution{
				ParentID:       "org-123",
				ParentName:     &acmeCorp,
				SubsidiaryID:   &teamID,
				SubsidiaryName: &engineering,
			},
			wantErr: false,
		},
		{
			name: "missing parent ID",
			attr: &Attribution{
				ParentName: &johnDoe,
			},
			wantErr: true,
		},
		{
			name: "parent ID too long",
			attr: &Attribution{
				ParentID: string(make([]byte, 101)),
			},
			wantErr: true,
		},
		{
			name: "100 characters of several bytes each",
			attr: &Attribution{
				ParentID:       strings.Repeat("😀", 100),
				ParentName:     &hundredChars,
				SubsidiaryID:   &hundredChars,
				SubsidiaryName: &hundredChars,
			},
			wantErr: false,
		},
		{
			name: "parent name too long",
			attr: &Attribution{
				ParentID:   "user-123",
				ParentName: &tooManyChars,
			},
			wantErr: true,
		},
		{
			name: "subsidiary name without its ID",
			attr: &Attribution{
				ParentID:       "org-123",
				SubsidiaryName: &engineering,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, err := SetAttribution(context.Background(), tt.attr)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetAttribution() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidAttribution) {
				t.Errorf("SetAttribution() error = %v, want one that matches ErrInvalidAttribution", err)
			}

			if got := attributionPayload(ctx); (got != nil) == tt.wantErr {
				t.Errorf("attribution = %v; want one only when SetAttribution succeeds", got)
			}
		})
	}
}

// A caller that reuses one Attribution for many requests changes none it
// already made, and cannot get past validate by changing it afterwards.
func TestSetAttributionKeepsItsValues(t *testing.T) {
	name := "Alice"
	attr := &Attribution{ParentID: "user-1", ParentName: &name}
	ctx, err := SetAttribution(context.Background(), attr)
	if err != nil {
		t.Fatal(err)
	}
	attr.ParentID = strings.Repeat("x", 500)
	name = "Bob"

	got, _ := json.Marshal(attributionPayload(ctx))
	if want := `{"parent":{"id":"user-1","name":"Alice"}}`; string(got) != want {
		t.Errorf("attribution = %s, want the values it had when set: %s", got, want)
	}
}

// A nil attribution removes the one an earlier SetAttribution put in the context.
func TestSetAttributionNil(t *testing.T) {
	ctx, err := SetAttribution(context.Background(), &Attribution{ParentID: "customer_123"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = SetAttribution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := attributionPayload(ctx); got != nil {
		t.Errorf("attribution = %v, want none", got)
	}
}
