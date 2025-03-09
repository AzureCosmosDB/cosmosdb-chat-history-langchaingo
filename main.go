package main

import (
	"log"
	"net/http"
	"os"

	"github.com/abhirockzz/langchaingo-cosmosdb-chat-history/server"
)

func main() {
	// Configure HTTP server
	mux := http.NewServeMux()

	// Serve static files
	mux.Handle("/", http.FileServer(http.Dir("./static")))
	
	// API endpoints
	mux.HandleFunc("/api/chat/start", server.HandleStartChat)
	mux.HandleFunc("/api/chat/stream", server.HandleStreamMessage)
	mux.HandleFunc("/api/chat/history", server.HandleGetHistory)
	mux.HandleFunc("/api/user/conversations", server.HandleListConversations)
	mux.HandleFunc("/api/chat/delete", server.HandleDeleteConversation)

	// Start the server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Web server starting on port %s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}