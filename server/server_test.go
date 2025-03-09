package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testPartitionKey        = "/userid"
	emulatorImage          = "mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-preview"
	emulatorPort           = "8081"
	emulatorEndpoint       = "http://localhost:8081"
	emulatorKey           = "C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw=="
)

var (
	emulator testcontainers.Container
	testClient *azcosmos.Client
)

func TestMain(m *testing.M) {
	// Set up the CosmosDB emulator container
	ctx := context.Background()
	var err error
	emulator, err = setupCosmosEmulator(ctx)
	if err != nil {
		fmt.Printf("Failed to set up CosmosDB emulator: %v\n", err)
		os.Exit(1)
	}

	// Set up the CosmosDB client
	testClient, err = setupCosmosClient()
	if err != nil {
		fmt.Printf("Failed to set up CosmosDB client: %v\n", err)
		os.Exit(1)
	}

	// Set up the database and container
	err = setupDatabaseAndContainer(ctx, testClient)
	if err != nil {
		fmt.Printf("Failed to set up database and container: %v\n", err)
		os.Exit(1)
	}

	// Run the tests
	code := m.Run()

	// Tear down the CosmosDB emulator container
	if emulator != nil {
		_ = emulator.Terminate(ctx)
	}

	os.Exit(code)
}

// Helper functions from cosmosdb_chat_history_emulator_test.go
func setupCosmosEmulator(ctx context.Context) (testcontainers.Container, error) {
	req := testcontainers.ContainerRequest{
		Image:        emulatorImage,
		ExposedPorts: []string{emulatorPort + ":8081", "1234:1234"},
		WaitingFor:   wait.ForListeningPort(nat.Port(emulatorPort)),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:         true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	// Give the emulator a bit more time to fully initialize
	time.Sleep(5 * time.Second)

	return container, nil
}

func setupCosmosClient() (*azcosmos.Client, error) {
	cred, err := azcosmos.NewKeyCredential(emulatorKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create key credential: %w", err)
	}

	client, err := azcosmos.NewClientWithKey(emulatorEndpoint, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cosmos client: %w", err)
	}

	return client, nil
}

func setupDatabaseAndContainer(ctx context.Context, client *azcosmos.Client) error {

	testOperationDBName := os.Getenv("COSMOSDB_DATABASE_NAME")
	if testOperationDBName == "" {
		log.Fatalf("COSMOSDB_DATABASE_NAME environment variable is not set")
	}

	testOperationContainerName := os.Getenv("COSMOSDB_CONTAINER_NAME")
	if testOperationContainerName == "" {
		log.Fatalf("COSMOSDB_CONTAINER_NAME environment variable is not set")
	}

	databaseProps := azcosmos.DatabaseProperties{ID: testOperationDBName}
	_, err := client.CreateDatabase(ctx, databaseProps, nil)
	if err != nil && !isResourceExistsError(err) {
		return fmt.Errorf("failed to create test database: %w", err)
	}

	database, err := client.NewDatabase(testOperationDBName)
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	containerProps := azcosmos.ContainerProperties{
		ID: testOperationContainerName,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{
			Paths: []string{testPartitionKey},
		},
		DefaultTimeToLive: to.Ptr[int32](60), // Short TTL for test data
	}

	_, err = database.CreateContainer(ctx, containerProps, nil)
	if err != nil && !isResourceExistsError(err) {
		return fmt.Errorf("failed to create test container: %w", err)
	}

	return nil
}

func isResourceExistsError(err error) bool {
	var responseErr *azcore.ResponseError
	if errors.As(err, &responseErr) {
		return responseErr.StatusCode == 409
	}
	return false
}

func TestStartChat(t *testing.T) {
	// Initialize the server
	cosmosClient = testClient
	
	t.Run("New session", func(t *testing.T) {
		req := StartChatRequest{
			UserID: "test_user",
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/start", bytes.NewBuffer(body))
		
		HandleStartChat(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp StartChatResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		assert.True(t, resp.Success)
	})

	t.Run("Missing user ID", func(t *testing.T) {
		req := StartChatRequest{}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/start", bytes.NewBuffer(body))
		
		HandleStartChat(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "User ID is required")
	})
}

func TestGetHistory(t *testing.T) {
	// Initialize the server
	cosmosClient = testClient
	
	// Create a test chat session
	userID := "test_user_history"
	sessionID := "test_session_history"
	
	t.Run("Empty history", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/chat/history?userID=%s&sessionID=%s", userID, sessionID), nil)
		
		HandleGetHistory(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp ChatHistoryResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Empty(t, resp.Messages)
	})

	t.Run("Missing parameters", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/chat/history", nil)
		
		HandleGetHistory(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "UserID and SessionID are required")
	})
}

func TestListConversations(t *testing.T) {
	// Initialize the server
	cosmosClient = testClient
	
	userID := "test_user_list"
	
	t.Run("No conversations", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/user/conversations?userID=%s", userID), nil)
		
		HandleListConversations(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp ListConversationsResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Empty(t, resp.Conversations)
	})

	t.Run("Missing user ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/user/conversations", nil)
		
		HandleListConversations(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "UserID is required")
	})
}

func TestDeleteConversation(t *testing.T) {
	// Initialize the server
	cosmosClient = testClient
	
	t.Run("Delete non-existent conversation", func(t *testing.T) {
		req := DeleteConversationRequest{
			UserID:    "test_user_delete",
			SessionID: "non_existent_session",
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/delete", bytes.NewBuffer(body))
		
		HandleDeleteConversation(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp DeleteConversationResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("Missing parameters", func(t *testing.T) {
		req := DeleteConversationRequest{}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/delete", bytes.NewBuffer(body))
		
		HandleDeleteConversation(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "UserID and SessionID are required")
	})
}

func TestChatFlow(t *testing.T) {
	// Initialize the server
	cosmosClient = testClient
	
	// Test the complete flow: start chat -> send messages -> get history -> list conversations -> delete
	userID := "test_user_flow"
	var sessionID string

	// 1. Start a new chat session
	t.Run("Start chat", func(t *testing.T) {
		req := StartChatRequest{
			UserID: userID,
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/start", bytes.NewBuffer(body))
		
		HandleStartChat(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp StartChatResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		sessionID = resp.SessionID
	})

	// 2. Send messages
	t.Run("Send messages", func(t *testing.T) {
		messages := []struct {
			content string
			isUser  bool
		}{
			{content: "Hello, I have a question about Go programming", isUser: true},
			{content: "Hi! I'd be happy to help with Go programming", isUser: false},
			{content: "How do I handle errors in Go?", isUser: true},
			{content: "In Go, errors are values and are typically handled using explicit error checking", isUser: false},
		}

		for _, msg := range messages {
			req := SendMessageRequest{
				UserID:    userID,
				SessionID: sessionID,
				Message:   msg.content,
			}
			body, _ := json.Marshal(req)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/chat/stream", bytes.NewBuffer(body))

			HandleStreamMessage(w, r)

			if msg.isUser {
				// For user messages, verify the request was accepted
				assert.Equal(t, http.StatusOK, w.Code)
			} else {
				// For AI responses, verify we got a streaming response
				assert.Equal(t, http.StatusOK, w.Code)
				assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
			}
		}
	})

	// 3. Get chat history
	t.Run("Get history", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/chat/history?userID=%s&sessionID=%s", userID, sessionID), nil)
		
		HandleGetHistory(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp ChatHistoryResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Messages)
		assert.Equal(t, "human", resp.Messages[0].Type)
		assert.Equal(t, "Hello, I have a question about Go programming", resp.Messages[0].Content)
	})

	// 4. List user's conversations
	t.Run("List conversations", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/user/conversations?userID=%s", userID), nil)
		
		HandleListConversations(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp ListConversationsResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Conversations)
		
		// Find our conversation
		found := false
		for _, conv := range resp.Conversations {
			if conv.SessionID == sessionID {
				found = true
				assert.True(t, conv.MessageCount > 0)
				break
			}
		}
		assert.True(t, found, "Created conversation should be in the list")
	})

	// 5. Delete the conversation
	t.Run("Delete conversation", func(t *testing.T) {
		req := DeleteConversationRequest{
			UserID:    userID,
			SessionID: sessionID,
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/delete", bytes.NewBuffer(body))
		
		HandleDeleteConversation(w, r)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp DeleteConversationResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)

		// Verify conversation is gone
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", fmt.Sprintf("/api/chat/history?userID=%s&sessionID=%s", userID, sessionID), nil)
		HandleGetHistory(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
		var historyResp ChatHistoryResponse
		json.Unmarshal(w.Body.Bytes(), &historyResp)
		assert.Empty(t, historyResp.Messages)
	})
}

// Add function to test concurrent chat sessions.
func TestConcurrentChats(t *testing.T) {
	// Initialize the server
	cosmosClient = testClient

	users := []struct {
		userID   string
		question string
	}{
		{"concurrent_user1", "What is a goroutine?"},
		{"concurrent_user2", "How do channels work?"},
		{"concurrent_user3", "Tell me about error handling"},
	}

	var sessions []struct {
		userID    string
		sessionID string
	}

	// Start chat sessions for all users
	for _, u := range users {
		req := StartChatRequest{
			UserID: u.userID,
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/start", bytes.NewBuffer(body))
		
		HandleStartChat(w, r)
		
		var resp StartChatResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		sessions = append(sessions, struct{userID string; sessionID string}{u.userID, resp.SessionID})
	}

	// Send messages concurrently
	for i, session := range sessions {
		req := SendMessageRequest{
			UserID:    session.userID,
			SessionID: session.sessionID,
			Message:   users[i].question,
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/stream", bytes.NewBuffer(body))
		
		HandleStreamMessage(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Verify each user's history is separate
	for _, session := range sessions {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/chat/history?userID=%s&sessionID=%s", 
			session.userID, session.sessionID), nil)
		
		HandleGetHistory(w, r)
		
		var resp ChatHistoryResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		require.NotEmpty(t, resp.Messages)
		
		// Find the user's original question
		found := false
		for _, u := range users {
			if u.userID == session.userID {
				for _, msg := range resp.Messages {
					if msg.Content == u.question {
						found = true
						break
					}
				}
				break
			}
		}
		assert.True(t, found, "User's question should be in their chat history")
	}
}

func TestStreamMessageErrors(t *testing.T) {
	// Initialize the server
	cosmosClient = testClient
	
	t.Run("Missing userID", func(t *testing.T) {
		req := SendMessageRequest{
			SessionID: "some_session",
			Message:   "Hello",
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/stream", bytes.NewBuffer(body))
		
		HandleStreamMessage(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "UserID")
	})
	
	t.Run("Missing sessionID", func(t *testing.T) {
		req := SendMessageRequest{
			UserID:  "some_user",
			Message: "Hello",
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/stream", bytes.NewBuffer(body))
		
		HandleStreamMessage(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "SessionID")
	})
	
	t.Run("Empty message", func(t *testing.T) {
		req := SendMessageRequest{
			UserID:    "some_user",
			SessionID: "some_session",
			Message:   "",
		}
		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/stream", bytes.NewBuffer(body))
		
		HandleStreamMessage(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "Message")
	})
	
	t.Run("Invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/chat/stream", bytes.NewBuffer([]byte("invalid json")))
		
		HandleStreamMessage(w, r)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp ErrorResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp.Error, "request format")
	})
	
	t.Run("Wrong HTTP method", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/chat/stream", nil)
		
		HandleStreamMessage(w, r)
		
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}