package main

import (
	"context"
	"fmt"
	"log"

	"github.com/CrystilAI/go-sdk"
)

func main() {
	crystilClient, err := crystil.New()
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	ctx := context.Background()

	workflows, err := crystilClient.Workflows().List(ctx)
	if err != nil {
		log.Fatal(err)
	}

	for _, workflow := range workflows {
		fmt.Printf("%+v\n", workflow)
	}

	//workflowClient := crystilClient.Workflow("workflow-uuid")
	//err = workflowClient.Update(ctx, "New Label")
	//if err != nil {
	//	log.Fatal(err)
	//}
}
