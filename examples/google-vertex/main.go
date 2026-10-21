package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/httptransport"
	crystil "github.com/CrystilAI/go-sdk"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	// Initialize Crystil client
	crystilClient, err := crystil.New(crystil.WithAPIKey(os.Getenv("CRYSTIL_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	// Find Google credentials: the file named by GOOGLE_APPLICATION_CREDENTIALS,
	// or the login saved by `gcloud auth application-default login`
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	})
	if err != nil {
		log.Fatal(err)
	}

	// genai adds no credentials to an HTTP client you give it, so build Google's
	// authenticated client; genai gets Crystil's copy of it below
	httpClient, err := httptransport.NewClient(&httptransport.Options{Credentials: creds})
	if err != nil {
		log.Fatal(err)
	}

	// A service-account file names its project; a gcloud login does not
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		if project, err = creds.ProjectID(ctx); err != nil {
			log.Fatal(err)
		}
		if project == "" {
			log.Fatal("set GOOGLE_CLOUD_PROJECT")
		}
	}
	location := os.Getenv("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = "us-central1"
	}

	// Create the Vertex AI client with the authenticated HTTP client
	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		HTTPClient: crystilClient.NewHTTPClient(crystil.ProviderGoogle, httpClient),
		Backend:    genai.BackendVertexAI,
		Project:    project,
		Location:   location,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Set attribution to track costs by user. It rides in ctx, so it applies to
	// the calls made with ctx
	aliceJohnson := "Alice Johnson"
	ctx, err = crystil.SetAttribution(ctx, &crystil.Attribution{
		ParentID:   "user-789",
		ParentName: &aliceJohnson,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use Vertex AI as normal - analytics tracked automatically
	resp, err := googleClient.Models.GenerateContent(
		ctx,
		"gemini-2.5-flash",
		genai.Text("What is the meaning of life?"),
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Response: %s\n", resp.Text())

	// Streamed calls are tracked too
	fmt.Print("Streamed: ")
	for chunk, err := range googleClient.Models.GenerateContentStream(
		ctx,
		"gemini-2.5-flash",
		genai.Text("Tell me a short joke"),
		nil,
	) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(chunk.Text())
	}
	fmt.Println()
}
