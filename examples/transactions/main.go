package main

import (
	"context"
	"fmt"
	"log"
	"os"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// user is someone the LLM calls are made for, and attributed to.
type user struct {
	id, name string
}

func main() {
	// Initialize Crystil client
	crystilClient, err := crystil.New(crystil.WithAPIKey(os.Getenv("CRYSTIL_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	// Create the OpenAI client once, on Crystil's wrapped HTTP client. Every
	// transaction below uses it.
	openaiClient := openai.NewClient(
		option.WithHTTPClient(crystilClient.HTTPClient(crystil.ProviderOpenAI)),
	)

	// Each request gets its own transaction, as each request a server handles would
	handleRequest(context.Background(), openaiClient, user{"user-123", "John Doe"},
		"What is the capital of France?", "What is the capital of Spain?")
	handleRequest(context.Background(), openaiClient, user{"user-456", "Jane Smith"},
		"What is 2 + 2?")
}

// handleRequest answers one user's questions in one transaction, attributed to
// that user.
func handleRequest(ctx context.Context, openaiClient openai.Client, u user, questions ...string) {
	// Start a transaction and set its attribution. They ride in ctx, so every
	// call made with it is recorded under them.
	ctx = crystil.NewTransaction(ctx)
	ctx, err := crystil.SetAttribution(ctx, &crystil.Attribution{
		ParentID:   u.id,
		ParentName: &u.name,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, question := range questions {
		resp, err := openaiClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model: openai.ChatModelGPT4oMini,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage(question),
			},
		})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s asked %q: %s\n", u.name, question, resp.Choices[0].Message.Content)
	}
}
