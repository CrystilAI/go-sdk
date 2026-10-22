//go:build integration

package integration

import (
	"os"
	"testing"

	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/httptransport"
	crystil "github.com/CrystilAI/go-sdk"
	"google.golang.org/genai"
)

func TestGemini(t *testing.T) {
	testGoogle(t, []string{"GOOGLE_API_KEY"}, func(t *testing.T, client *crystil.Client) *genai.ClientConfig {
		return &genai.ClientConfig{
			Backend:    genai.BackendGeminiAPI,
			APIKey:     os.Getenv("GOOGLE_API_KEY"),
			HTTPClient: client.HTTPClient(crystil.ProviderGoogle),
		}
	})
}

// TestVertex reads the project from the service-account file, unless
// GOOGLE_CLOUD_PROJECT is set.
func TestVertex(t *testing.T) {
	testGoogle(t, []string{"GOOGLE_APPLICATION_CREDENTIALS"}, func(t *testing.T, client *crystil.Client) *genai.ClientConfig {
		creds, err := credentials.DetectDefault(&credentials.DetectOptions{
			Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
		})
		if err != nil {
			t.Fatal(err)
		}
		project := os.Getenv("GOOGLE_CLOUD_PROJECT")
		if project == "" {
			if project, err = creds.ProjectID(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		// genai adds no credentials to a caller's HTTP client, so a Vertex caller
		// gives it Crystil's copy of Google's authenticated client.
		httpClient, err := httptransport.NewClient(&httptransport.Options{Credentials: creds})
		if err != nil {
			t.Fatal(err)
		}
		return &genai.ClientConfig{
			Backend:    genai.BackendVertexAI,
			Project:    project,
			Location:   model("GOOGLE_CLOUD_LOCATION", "us-central1"),
			HTTPClient: client.NewHTTPClient(crystil.ProviderGoogle, httpClient),
		}
	})
}

// testGoogle runs the Gemini calls through a genai client built by config.
func testGoogle(t *testing.T, keys []string, config func(*testing.T, *crystil.Client) *genai.ClientConfig) {
	m := model("GOOGLE_MODEL", "gemini-2.5-flash")
	chat := &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser)}
	tools := &genai.GenerateContentConfig{
		Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:                 weatherTool,
			Description:          weatherDescription,
			ParametersJsonSchema: map[string]any{"type": "object", "properties": weatherProperties, "required": []string{"location"}},
		}}}},
		ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}},
	}

	cases := []struct {
		name   string
		prompt string
		config *genai.GenerateContentConfig
		stream bool
	}{
		{"generate", userPrompt, chat, false},
		{"generate stream", userPrompt, chat, true},
		{"tools", toolPrompt, tools, false},
		{"tools stream", toolPrompt, tools, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, client, rec := start(t, keys...)
			gc, err := genai.NewClient(t.Context(), config(t, client))
			if err != nil {
				t.Fatal(err)
			}

			var answers int
			if tc.stream {
				for resp, err := range gc.Models.GenerateContentStream(ctx, m, genai.Text(tc.prompt), tc.config) {
					if err != nil {
						t.Fatal(err)
					}
					answers += len(resp.Candidates)
				}
			} else {
				resp, err := gc.Models.GenerateContent(ctx, m, genai.Text(tc.prompt), tc.config)
				if err != nil {
					t.Fatal(err)
				}
				answers = len(resp.Candidates)
			}
			if answers == 0 {
				t.Fatal("model gave no answer")
			}

			p := rec.payload(t)
			checkPayload(t, p, string(crystil.ProviderGoogle), m)
			if tc.stream {
				checkEventList(t, p)
			}
		})
	}
}
