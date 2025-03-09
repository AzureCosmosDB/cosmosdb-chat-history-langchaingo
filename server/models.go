package server

// Request and response types
type StartChatRequest struct {
	UserID    string `json:"userID"`
	SessionID string `json:"sessionID"`
}

type StartChatResponse struct {
	SessionID string `json:"sessionID"`
	Success   bool   `json:"success"`
}

type SendMessageRequest struct {
	UserID    string `json:"userID"`
	SessionID string `json:"sessionID"`
	Message   string `json:"message"`
}

type SendMessageResponse struct {
	Response string `json:"response"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type MessageInfo struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type ChatHistoryResponse struct {
	Messages []MessageInfo `json:"messages"`
}

// New response type for conversations list
type ConversationInfo struct {
	SessionID     string `json:"sessionID"`
	MessageCount  int    `json:"messageCount"`
}

type ListConversationsResponse struct {
	Conversations []ConversationInfo `json:"conversations"`
}

// New request type for deleting a conversation
type DeleteConversationRequest struct {
	UserID    string `json:"userID"`
	SessionID string `json:"sessionID"`
}

type DeleteConversationResponse struct {
	Success bool `json:"success"`
}
