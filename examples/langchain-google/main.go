package main

import (
	"context"
	"fmt"
	"log"
	"os"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

func main() {
	// Initialize Crystil client
	crystilClient, err := crystil.New(crystil.WithAPIKey(os.Getenv("CRYSTIL_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	// Set attribution to track costs by user (affects all future requests)
	langchainUser := "LangChain User"
	err = crystilClient.SetAttribution(&crystil.Attribution{
		ParentID:   "user-langchain",
		ParentName: &langchainUser,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Google's library adds no API key to an HTTP client you give it, so build
	// Google's client, which puts the key on each request
	apiKey := os.Getenv("GOOGLE_API_KEY")
	httpClient, _, err := htransport.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatal(err)
	}

	// The library still needs the key for a client it builds without yours.
	// WithRest keeps its calls on HTTP, where Crystil can see them.
	llm, err := googleai.New(ctx,
		googleai.WithAPIKey(apiKey),
		googleai.WithDefaultModel("gemini-2.5-flash"),
		googleai.WithRest(),
		googleai.WithHTTPClient(crystilClient.NewHTTPClient(crystil.ProviderGoogle, httpClient)),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Use LangChain as normal - analytics tracked automatically
	response, err := llms.GenerateFromSinglePrompt(ctx, llm, "What are the three laws of robotics?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Response: %s\n", response)

	// Streamed calls are tracked too. The library can stop a stream early without
	// closing it, as for a blocked answer; canceling its context still records it
	streamCtx, cancel := context.WithCancel(ctx)
	fmt.Print("Streamed: ")
	_, err = llms.GenerateFromSinglePrompt(streamCtx, llm, "Explain quantum computing in one sentence",
		llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			fmt.Print(string(chunk))
			return nil
		}),
	)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println()
}
