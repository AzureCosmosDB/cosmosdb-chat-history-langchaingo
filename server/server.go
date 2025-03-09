package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/abhirockzz/langchaingo-cosmosdb-chat-history/cosmosdb"
	"github.com/google/uuid"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/memory"
	"github.com/tmc/langchaingo/outputparser"
	"github.com/tmc/langchaingo/prompts"
)

const (
	template           = "{{.chat_history}}\n{{.human_input}}"
	// databaseName       = "chat_history_langchaingo"
	// containerName      = "chats"
)

var (
	databaseName	   string
	containerName      string
	cosmosDBEndpoint string
	cosmosDBConnectionString string
	llm     *openai.LLM
	cosmosClient *azcosmos.Client
	promptsTemplate prompts.PromptTemplate
)

// Global session map to manage active LLM chains
// In a production app, you might want something more robust
var activeChains = make(map[string]*chains.LLMChain)

func init() {

	cosmosDBConnectionString = os.Getenv("COSMOSDB_CONNECTION_STRING")
	if cosmosDBConnectionString == "" {
		//log.Fatalf("COSMOSDB_ENDPOINT_URL environment variable is not set")
		//log.Println("COSMOSDB_CONNECTION_STRING environment variable is not set, using COSMOSDB_ENDPOINT_URL instead")

		cosmosDBEndpoint = os.Getenv("COSMOSDB_ENDPOINT_URL")
		if cosmosDBEndpoint == "" {
			log.Fatalf("You must set either COSMOSDB_CONNECTION_STRING or COSMOSDB_ENDPOINT_URL")
		}
	}

	// we don't need the endpoint and key, but they should be configured as environment variables for langchaingo
	if os.Getenv("OPENAI_BASE_URL") == "" {
		log.Fatalf("OPENAI_BASE_URL environment variable is not set")
	}

	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatalf("OPENAI_API_KEY environment variable is not set")
	}

	modelName := os.Getenv("AZURE_OPENAI_MODEL_NAME")
	if modelName == "" {
		log.Fatalf("AZURE_OPENAI_MODEL_NAME environment variable is not set")
	}
	
	// embeddingModelName := os.Getenv("AZURE_OPENAI_EMBEDDING_MODEL_NAME")
	// if embeddingModelName == "" {
	// 	log.Fatalf("AZURE_OPENAI_EMBEDDING_MODEL_NAME environment variable is not set")
	// }

	databaseName = os.Getenv("COSMOSDB_DATABASE_NAME")
	if databaseName == "" {
		log.Fatalf("COSMOSDB_DATABASE_NAME environment variable is not set")
	}

	containerName = os.Getenv("COSMOSDB_CONTAINER_NAME")
	if containerName == "" {
		log.Fatalf("COSMOSDB_CONTAINER_NAME environment variable is not set")
	}

	var err error

	// Initialize the Azure OpenAI LLM
    llm, err = openai.New(
        openai.WithAPIType(openai.APITypeAzure),
        openai.WithModel(modelName),
		// an embedding model is not actually required but has been added because langchaingo requires it
        openai.WithEmbeddingModel("dummy_value"),
    )
    if err != nil {
        log.Fatalf("Failed to initialize Azure OpenAI LLM: %v", err)
    }

	// Initialize the prompt template
	promptsTemplate = prompts.NewPromptTemplate(
		template,
		[]string{"chat_history", "human_input"},
	)

	// Initialize the CosmosDB client
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatal("Failed to create Azure credential:", err)
	}

	if cosmosDBConnectionString != "" {
		cosmosClient, err = azcosmos.NewClientFromConnectionString(cosmosDBConnectionString, nil)
	} else {
		cosmosClient, err = azcosmos.NewClient(cosmosDBEndpoint, credential, nil)
	}

	if err != nil {
		log.Fatal("Failed to create CosmosDB client:", err)
	}

	log.Println("Initialized LLM and CosmosDB client")
}

func HandleStartChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req StartChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Validate user ID
	if req.UserID == "" {
		sendErrorResponse(w, "User ID is required", http.StatusBadRequest)
		return
	}

	// Generate a new session ID if one wasn't provided
	if req.SessionID == "" {
		req.SessionID = uuid.NewString()
	}

	// Create a chat history instance
	cosmosChatHistory, err := cosmosdb.NewCosmosDBChatMessageHistory(cosmosClient, databaseName, containerName, req.SessionID, req.UserID)
	if err != nil {
		log.Printf("Error creating chat history: %v", err)
		sendErrorResponse(w, "Failed to create chat session", http.StatusInternalServerError)
		return
	}

	// Create a memory with the chat history
	chatMemory := memory.NewConversationBuffer(
		memory.WithMemoryKey("chat_history"),
		memory.WithChatHistory(cosmosChatHistory),
	)

	// Create an LLM chain
	chain := chains.LLMChain{
		Prompt:       promptsTemplate,
		LLM:          llm,
		Memory:       chatMemory,
		OutputParser: outputparser.NewSimple(),
		OutputKey:    "text",
	}

	// Store the chain for later use
	sessionKey := fmt.Sprintf("%s:%s", req.UserID, req.SessionID)
	activeChains[sessionKey] = &chain

	response := StartChatResponse{
		SessionID: req.SessionID,
		Success:   true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func HandleStreamMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Validate fields
	if req.UserID == "" || req.SessionID == "" || req.Message == "" {
		sendErrorResponse(w, "UserID, SessionID, and Message are required", http.StatusBadRequest)
		return
	}

	// Set up streaming response
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	// Create a flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Get or create the chain for this session
	sessionKey := fmt.Sprintf("%s:%s", req.UserID, req.SessionID)
	chain, exists := activeChains[sessionKey]

	if !exists {
		// If chain doesn't exist, create a new one
		cosmosChatHistory, err := cosmosdb.NewCosmosDBChatMessageHistory(cosmosClient, databaseName, containerName, req.SessionID, req.UserID)
		if err != nil {
			log.Printf("Error creating chat history: %v", err)
			http.Error(w, "Failed to create chat session", http.StatusInternalServerError)
			return
		}

		chatMemory := memory.NewConversationBuffer(
			memory.WithMemoryKey("chat_history"),
			memory.WithChatHistory(cosmosChatHistory),
		)

		newChain := chains.LLMChain{
			Prompt:       promptsTemplate,
			LLM:          llm,
			Memory:       chatMemory,
			OutputParser: outputparser.NewSimple(),
			OutputKey:    "text",
		}

		chain = &newChain
		activeChains[sessionKey] = chain
	}

	// Create a context that can be canceled
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Keep track of the full response to verify it was saved correctly
	var fullResponse string

	// Stream the response using the chain
	_, err := chains.Call(ctx, *chain, 
		map[string]any{"human_input": req.Message}, 
		chains.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			// Write the chunk to the response
			_, err := w.Write(chunk)
			if err != nil {
				return err
			}
			
			// Collect the full response
			fullResponse += string(chunk)
			
			// Flush the buffer to send the chunk immediately
			flusher.Flush()
			return nil
		}),
	)

	if err != nil {
		log.Printf("Error streaming response: %v", err)
		//errorOccurred = true
		
		// Send an error message to the client if we haven't already sent a partial response
		if fullResponse == "" {
			// Clean up the error message for display to the user
			errorMsg := err.Error()
			if isContentFilterError := strings.Contains(strings.ToLower(errorMsg), "content management policy"); isContentFilterError {
				errorMsg = "I apologize, but I can't respond to that request as it triggered the content filter. Please try rephrasing your question."
			} else {
				errorMsg = "I apologize, but I encountered an error processing your request. Please try again later."
			}
			
			// Return the error as plain text since we've already set the response headers
			w.Write([]byte(errorMsg))
			flusher.Flush()
		}
	}
}

func HandleGetHistory(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	userID := r.URL.Query().Get("userID")
	sessionID := r.URL.Query().Get("sessionID")

	if userID == "" || sessionID == "" {
		sendErrorResponse(w, "UserID and SessionID are required", http.StatusBadRequest)
		return
	}

	// Create a chat history instance
	cosmosChatHistory, err := cosmosdb.NewCosmosDBChatMessageHistory(cosmosClient, databaseName, containerName, sessionID, userID)
	if err != nil {
		log.Printf("Error creating chat history: %v", err)
		sendErrorResponse(w, "Failed to access chat history", http.StatusInternalServerError)
		return
	}

	// Get the messages
	messages, err := cosmosChatHistory.Messages(context.Background())
	if err != nil {
		log.Printf("Error retrieving messages: %v", err)
		sendErrorResponse(w, "Failed to retrieve chat history", http.StatusInternalServerError)
		return
	}

	// Transform the messages into a format suitable for the frontend
	var messageInfos []MessageInfo
	for _, msg := range messages {
		messageType := "unknown"
		
		switch msg.GetType() {
		case llms.ChatMessageTypeHuman:
			messageType = "human"
		case llms.ChatMessageTypeAI:
			messageType = "ai"
		case llms.ChatMessageTypeSystem:
			messageType = "system"
		}

		messageInfos = append(messageInfos, MessageInfo{
			Type:    messageType,
			Content: msg.GetContent(),
		})
	}

	response := ChatHistoryResponse{
		Messages: messageInfos,
	}

	end := time.Now()
	log.Printf("Retrieved %d messages for session %s in %s", len(messages), sessionID, end.Sub(start))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Function to handle retrieving all conversations for a user
func HandleListConversations(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("userID")
	if userID == "" {
		sendErrorResponse(w, "UserID is required", http.StatusBadRequest)
		return
	}

	query := fmt.Sprintf("SELECT * FROM c WHERE c.userid = '%s'", userID)
	database, err := cosmosClient.NewDatabase(databaseName)
	if err != nil {
		log.Printf("Error getting database: %v", err)
		sendErrorResponse(w, "Failed to access database", http.StatusInternalServerError)
		return
	}

	container, err := database.NewContainer(containerName)
	if err != nil {
		log.Printf("Error getting container: %v", err)
		sendErrorResponse(w, "Failed to access container", http.StatusInternalServerError)
		return
	}

	pk := azcosmos.NewPartitionKeyString(userID)
	queryPager := container.NewQueryItemsPager(query, pk, nil)
	var conversations []ConversationInfo

	for queryPager.More() {
		queryResponse, err := queryPager.NextPage(r.Context())
		if err != nil {
			log.Printf("Error querying for conversations: %v", err)
			sendErrorResponse(w, "Failed to retrieve conversations", http.StatusInternalServerError)
			return
		}

		var items []map[string]any
		for _, itemBytes := range queryResponse.Items {
			var item map[string]any
			err = json.Unmarshal(itemBytes, &item)
			if err != nil {
				log.Printf("Error unmarshalling item: %v", err)
				continue
			}
			items = append(items, item)
		}

		for _, item := range items {
			sessionID, ok := item["id"].(string)
			if !ok {
				continue
			}

			conv := ConversationInfo{
				SessionID: sessionID,
				MessageCount: 0,
			}

			if messages, ok := item["messages"].([]any); ok {
				conv.MessageCount = len(messages)
			}

			conversations = append(conversations, conv)
		}
	}

	response := ListConversationsResponse{
		Conversations: conversations,
	}

	end := time.Now()
	log.Printf("%d conversations retrieved for %s in %s", len(conversations), userID, end.Sub(start))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func HandleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req DeleteConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format", http.StatusBadRequest)
		return
	}
	
	// Validate fields
	if req.UserID == "" || req.SessionID == "" {
		sendErrorResponse(w, "UserID and SessionID are required", http.StatusBadRequest)
		return
	}
	
	// Create a chat history instance
	cosmosChatHistory, err := cosmosdb.NewCosmosDBChatMessageHistory(cosmosClient, databaseName, containerName, req.SessionID, req.UserID)
	if err != nil {
		log.Printf("Error creating chat history: %v", err)
		sendErrorResponse(w, "Failed to access chat history", http.StatusInternalServerError)
		return
	}
	
	// Delete the conversation using the Clear method
	err = cosmosChatHistory.Clear(context.Background())
	if err != nil {
		log.Printf("Error deleting conversation: %v", err)
		sendErrorResponse(w, "Failed to delete conversation", http.StatusInternalServerError)
		return
	}
	
	// Remove the session from active chains if it exists
	sessionKey := fmt.Sprintf("%s:%s", req.UserID, req.SessionID)
	delete(activeChains, sessionKey)
	
	response := DeleteConversationResponse{
		Success: true,
	}
	
	end := time.Now()
	log.Printf("Deleted conversation %s for user %s in %s", req.SessionID, req.UserID, end.Sub(start))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Helper function to send error responses
func sendErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
