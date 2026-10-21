package main

import (
	"context"
	"fmt"
	"log"
	"os"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

func main() {
	// Initialize Crystil client
	crystilClient, err := crystil.New(crystil.WithAPIKey(os.Getenv("CRYSTIL_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	ctx := context.Background()

	// Set attribution to track costs by user. It rides in ctx, so it applies to
	// the calls made with ctx
	langchainUser := "LangChain User"
	ctx, err = crystil.SetAttribution(ctx, &crystil.Attribution{
		ParentID:   "user-langchain",
		ParentName: &langchainUser,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Give LangChain the Crystil HTTP client, as with any provider SDK.
	// Crystil sees that langchaingo sent the request and records it as LangChain's.
	llm, err := openai.New(
		openai.WithModel("gpt-4o-mini"),
		openai.WithToken(os.Getenv("OPENAI_API_KEY")),
		openai.WithHTTPClient(crystilClient.HTTPClient(crystil.ProviderOpenAI)),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Use LangChain as normal - analytics tracked automatically
	response, err := llms.GenerateFromSinglePrompt(
		ctx,
		llm,
		"What are the three laws of robotics?",
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", response)

	// Start a new transaction, attributed to another user, for the next request.
	// The same LLM records it: the transaction and attribution ride in the context.
	ctx = crystil.NewTransaction(ctx)
	otherUser := "Other User"
	ctx, err = crystil.SetAttribution(ctx, &crystil.Attribution{
		ParentID:   "user-other",
		ParentName: &otherUser,
	})
	if err != nil {
		log.Fatal(err)
	}

	response2, err := llms.GenerateFromSinglePrompt(
		ctx,
		llm,
		"Explain quantum computing in one sentence",
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", response2)

	// All LLM calls are automatically tracked through the Crystil HTTP client!
}
