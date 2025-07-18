package main

import (
	"log"
	"net/http"
	"os"

	"github.com/abhirockzz/cosmosdb-go-sdk-helper/auth"
	"github.com/abhirockzz/langchaingo-cosmosdb-chat-history/server"
	"github.com/tmc/langchaingo/llms/openai"
)

func main() {
	// Configure HTTP server
	mux := http.NewServeMux()

	// Serve static files
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	databaseName := os.Getenv("COSMOSDB_DATABASE_NAME")
	if databaseName == "" {
		log.Fatalf("COSMOSDB_DATABASE_NAME environment variable is not set")
	}

	containerName := os.Getenv("COSMOSDB_CONTAINER_NAME")
	if containerName == "" {
		log.Fatalf("COSMOSDB_CONTAINER_NAME environment variable is not set")
	}

	cosmosDBEndpoint := os.Getenv("COSMOSDB_ENDPOINT_URL")
	if cosmosDBEndpoint == "" {
		log.Fatalf("You must set either COSMOSDB_CONNECTION_STRING or COSMOSDB_ENDPOINT_URL")
	}

	client, err := auth.GetCosmosDBClient(cosmosDBEndpoint, false, nil)
	if err != nil {
		log.Fatal(err)
	}

	azOpenAIEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	if azOpenAIEndpoint == "" {
		log.Fatalf("AZURE_OPENAI_ENDPOINT environment variable is not set")
	}

	azOpenAIKey := os.Getenv("AZURE_OPENAI_KEY")
	if azOpenAIKey == "" {
		log.Fatalf("AZURE_OPENAI_KEY environment variable is not set")
	}

	modelName := os.Getenv("AZURE_OPENAI_MODEL_NAME")
	if modelName == "" {
		log.Fatalf("AZURE_OPENAI_MODEL_NAME environment variable is not set")
	}

	// Initialize the Azure OpenAI LLM
	llm, err := openai.New(
		openai.WithAPIType(openai.APITypeAzure),
		openai.WithBaseURL(azOpenAIEndpoint),
		openai.WithToken(azOpenAIKey),
		openai.WithModel(modelName),
		// an embedding model is not actually required but has been added because langchaingo requires it
		openai.WithEmbeddingModel("dummy_value"),
	)
	if err != nil {
		log.Fatalf("Failed to initialize Azure OpenAI LLM: %v", err)
	}

	app, err := server.New(databaseName, containerName, client, llm)
	if err != nil {
		log.Fatalf("Failed to initialize Azure OpenAI LLM: %v", err)
	}

	// API endpoints
	mux.HandleFunc("/api/chat/start", app.HandleStartChat)
	mux.HandleFunc("/api/chat/stream", app.HandleStreamMessage)
	mux.HandleFunc("/api/chat/history", app.HandleGetHistory)
	mux.HandleFunc("/api/user/conversations", app.HandleListConversations)
	mux.HandleFunc("/api/chat/delete", app.HandleDeleteConversation)

	// Start the server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Web server starting on port %s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
