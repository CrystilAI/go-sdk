package main

import (
	"context"
	"log"
	"time"

	"github.com/CrystilAI/go-sdk"
	"github.com/CrystilAI/go-sdk/api"
)

func main() {
	crystilClient, err := crystil.New()
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	ctx := context.Background()

	workflowClient := crystilClient.Workflow("[Workflow UUID]")
	summary, summaryErr := workflowClient.Invocation().Summary(ctx, api.SummaryOptions{
		DateStart: time.Date(2011, 2, 23, 0, 0, 0, 0, time.UTC),
	})
	if summaryErr != nil {
		log.Fatal(err)
	}

	log.Printf("summary: %+v", summary)
}
