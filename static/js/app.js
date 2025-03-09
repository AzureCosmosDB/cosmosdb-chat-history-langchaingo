document.addEventListener('DOMContentLoaded', () => {
    // DOM elements
    const loginContainer = document.getElementById('login-container');
    const chatContainer = document.getElementById('chat-container');
    const startChatBtn = document.getElementById('start-chat-btn');
    const signOutBtn = document.getElementById('sign-out-btn');
    const newConversationBtn = document.getElementById('new-conversation-btn');
    const sendBtn = document.getElementById('send-btn');
    const messageInput = document.getElementById('message-input');
    const chatMessages = document.getElementById('chat-messages');
    const conversationsList = document.getElementById('conversations-list');
    const noConversations = document.getElementById('no-conversations');
    const displayUserID = document.getElementById('display-user-id');
    const loadingOverlay = document.getElementById('loading-overlay');
    const userIDInput = document.getElementById('user-id');
    
    // Spinner elements
    const loginSpinner = document.getElementById('login-spinner');
    const newConvoSpinner = document.getElementById('new-convo-spinner');
    const sendSpinner = document.getElementById('send-spinner');
    
    let currentUserID = '';
    let currentSessionID = '';
    let userConversations = [];
    let currentStreamingMessageElement = null;
    let deletingSessionID = ''; // Track which session is being deleted

    // Initialize event listeners
    startChatBtn.addEventListener('click', handleStartChat);
    signOutBtn.addEventListener('click', handleSignOut);
    newConversationBtn.addEventListener('click', handleNewConversation);
    sendBtn.addEventListener('click', handleSendMessage);
    messageInput.addEventListener('keypress', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSendMessage();
        }
    });
    
    // Add focus states and keyboard accessibility
    userIDInput.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') {
            handleStartChat();
        }
    });

    // Configure Marked.js options
    marked.setOptions({
        breaks: true,        // Enable line breaks
        gfm: true,           // Enable GitHub Flavored Markdown
        headerIds: false,    // Disable header IDs for better security
        sanitize: true       // Enable sanitization to prevent XSS attacks (if using an older version)
    });

    // Helper function to show loading overlay
    function showLoadingOverlay() {
        loadingOverlay.style.display = 'flex';
    }

    // Helper function to hide loading overlay
    function hideLoadingOverlay() {
        loadingOverlay.style.display = 'none';
    }

    // Helper function to show a button spinner
    function showButtonSpinner(button, spinner) {
        button.disabled = true;
        spinner.style.display = 'inline-block';
    }

    // Helper function to hide a button spinner
    function hideButtonSpinner(button, spinner) {
        button.disabled = false;
        spinner.style.display = 'none';
    }

    // Handle starting a chat session
    async function handleStartChat() {
        const userID = userIDInput.value.trim();
        
        if (!userID) {
            showInputError(userIDInput, 'Please enter a user ID');
            return;
        }
        
        // Show loading indicators
        showButtonSpinner(startChatBtn, loginSpinner);
        
        try {
            // Remember user ID and display it
            currentUserID = userID;
            displayUserID.textContent = currentUserID;
            
            // Show loading overlay during the transition
            showLoadingOverlay();
            
            // Fetch user's existing conversations first
            await fetchUserConversations();
            
            // Hide login and show chat interface
            loginContainer.style.display = 'none';
            chatContainer.style.display = 'flex';
            
            // Always start a new conversation immediately when logging in
            // This is more intuitive than requiring the user to select something first
            await startOrJoinChat('');
            
        } catch (error) {
            console.error('Error starting chat:', error);
            showToast('Failed to connect to the server. Please try again.', 'error');
        } finally {
            // Hide loading indicators
            hideButtonSpinner(startChatBtn, loginSpinner);
            hideLoadingOverlay();
        }
    }

    // Show input error
    function showInputError(inputElement, message) {
        // Create or get error message element
        let errorElement = inputElement.parentNode.querySelector('.input-error');
        
        if (!errorElement) {
            errorElement = document.createElement('div');
            errorElement.classList.add('input-error');
            inputElement.parentNode.appendChild(errorElement);
        }
        
        errorElement.textContent = message;
        inputElement.classList.add('error');
        
        // Remove error after 3 seconds
        setTimeout(() => {
            if (errorElement) {
                errorElement.textContent = '';
                inputElement.classList.remove('error');
            }
        }, 3000);
    }
    
    // Show toast notification
    function showToast(message, type = 'info') {
        // Create toast container if it doesn't exist
        let toastContainer = document.querySelector('.toast-container');
        
        if (!toastContainer) {
            toastContainer = document.createElement('div');
            toastContainer.classList.add('toast-container');
            document.body.appendChild(toastContainer);
        }
        
        // Create toast element
        const toast = document.createElement('div');
        toast.classList.add('toast', `toast-${type}`);
        
        // Add icon based on type
        let icon = 'info-circle';
        if (type === 'error') icon = 'exclamation-circle';
        if (type === 'success') icon = 'check-circle';
        
        toast.innerHTML = `
            <i class="fas fa-${icon}"></i>
            <span>${message}</span>
        `;
        
        // Add to container
        toastContainer.appendChild(toast);
        
        // Show with animation
        setTimeout(() => {
            toast.classList.add('show');
        }, 10);
        
        // Remove after 3 seconds
        setTimeout(() => {
            toast.classList.remove('show');
            toast.addEventListener('transitionend', () => {
                toast.remove();
            });
        }, 3000);
    }

    // Start a new chat or join an existing one with a given session ID
    async function startOrJoinChat(sessionID) {
        try {
            // If called from the new conversation button, show its spinner
            if (!sessionID) {
                showButtonSpinner(newConversationBtn, newConvoSpinner);
            }
            
            const response = await fetch('/api/chat/start', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ 
                    userID: currentUserID, 
                    sessionID 
                }),
            });
            
            const data = await response.json();
            
            if (response.ok) {
                currentSessionID = data.sessionID;
                
                // Update sidebar to highlight active conversation
                updateConversationsUI();
                
                // Load existing messages if any
                await loadChatHistory();
                
                // Enable chat input
                messageInput.disabled = false;
                sendBtn.disabled = false;
                
                // Focus on the message input
                messageInput.focus();
                
                // Removed toast notification for new conversations
            } else {
                showToast(`Error: ${data.error || 'Failed to start chat'}`, 'error');
            }
        } catch (error) {
            console.error('Error starting/joining chat:', error);
            showToast('Failed to start or join the chat. Please try again.', 'error');
        } finally {
            // Hide spinner if it was shown
            if (!sessionID) {
                hideButtonSpinner(newConversationBtn, newConvoSpinner);
            }
        }
    }

    // Handle creating a new chat session from sidebar button
    function handleNewConversation() {
        currentSessionID = '';
        startOrJoinChat('');  // Empty session ID means server will generate a new one
    }

    // Handle user signing out
    function handleSignOut() {
        currentUserID = '';
        currentSessionID = '';
        userConversations = [];
        chatMessages.innerHTML = '';
        loginContainer.style.display = 'block';
        chatContainer.style.display = 'none';
        userIDInput.value = '';
        userIDInput.focus();
        
        // Removed toast notification for signing out
    }

    // Handle sending a message
    async function handleSendMessage() {
        const message = messageInput.value.trim();
        if (!message) return;
        
        // Clear input and disable button
        messageInput.value = '';
        showButtonSpinner(sendBtn, sendSpinner);
        
        // Add user message to UI
        appendMessage('user', message);
        
        // Prepare for streaming response
        appendStreamingMessage();
        
        try {
            // Use fetch with streaming for the chat message
            const response = await fetch('/api/chat/stream', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    userID: currentUserID,
                    sessionID: currentSessionID,
                    message,
                }),
            });
            
            if (!response.ok) {
                throw new Error(`HTTP error! Status: ${response.status}`);
            }
            
            // Get the response reader for streaming
            const reader = response.body.getReader();
            let receivedText = "";
            
            // Process the stream
            while (true) {
                const { value, done } = await reader.read();
                
                if (done) break;
                
                // Decode the received chunk
                const chunk = new TextDecoder().decode(value);
                receivedText += chunk;
                
                // Update the streaming message with the accumulated text
                updateStreamingMessage(receivedText);
            }
            
            // Complete the streaming message
            finalizeStreamingMessage();
            
            // Fetch conversations again to update the sidebar with latest message
            await fetchUserConversations();
        } catch (error) {
            // Remove streaming indicator if there was an error
            removeStreamingMessage();
            console.error('Error sending message:', error);
            appendMessage('ai', "I'm sorry, I encountered an error processing your request. Please try again.");
            showToast('Failed to send message. Please try again.', 'error');
        } finally {
            // Re-enable the send button
            hideButtonSpinner(sendBtn, sendSpinner);
        }
    }

    // Append a placeholder for the streaming message
    function appendStreamingMessage() {
        const messageDiv = document.createElement('div');
        messageDiv.classList.add('message', 'ai-message', 'streaming-message');
        messageDiv.id = 'streaming-message';
        
        const messageContent = document.createElement('div');
        messageContent.classList.add('message-content');
        
        // Start with typing indicator
        const typingIndicator = document.createElement('div');
        typingIndicator.classList.add('typing-indicator');
        typingIndicator.id = 'typing-indicator';
        
        const dot1 = document.createElement('span');
        const dot2 = document.createElement('span');
        const dot3 = document.createElement('span');
        dot1.classList.add('dot');
        dot2.classList.add('dot');
        dot3.classList.add('dot');
        
        typingIndicator.appendChild(dot1);
        typingIndicator.appendChild(dot2);
        typingIndicator.appendChild(dot3);
        
        messageContent.appendChild(typingIndicator);
        messageDiv.appendChild(messageContent);
        
        chatMessages.appendChild(messageDiv);
        currentStreamingMessageElement = messageContent;
        
        // Add animation class
        messageDiv.classList.add('message-appear');
        
        scrollToBottom();
    }

    // Update the streaming message with new content
    function updateStreamingMessage(text) {
        if (!currentStreamingMessageElement) return;
        
        // Remove typing indicator if it exists
        const typingIndicator = document.getElementById('typing-indicator');
        if (typingIndicator) {
            typingIndicator.remove();
        }
        
        // Render content as Markdown when it's an AI response
        currentStreamingMessageElement.innerHTML = marked.parse(text);
        scrollToBottom();
    }

    // Finalize the streaming message when complete
    function finalizeStreamingMessage() {
        if (!currentStreamingMessageElement) return;
        
        const messageDiv = document.getElementById('streaming-message');
        if (messageDiv) {
            messageDiv.id = ''; // Remove the streaming ID
            messageDiv.classList.remove('streaming-message');
        }
        
        currentStreamingMessageElement = null;
        scrollToBottom();
    }

    // Remove the streaming message (used in case of errors)
    function removeStreamingMessage() {
        const messageDiv = document.getElementById('streaming-message');
        if (messageDiv) {
            messageDiv.remove();
        }
        currentStreamingMessageElement = null;
    }

    // Fetch all conversations for the current user
    async function fetchUserConversations() {
        try {
            const response = await fetch(`/api/user/conversations?userID=${currentUserID}`);
            
            if (response.ok) {
                const data = await response.json();
                userConversations = data.conversations || [];
                updateConversationsUI();
            } else {
                console.error('Failed to fetch conversations');
                showToast('Failed to load conversations', 'error');
            }
        } catch (error) {
            console.error('Error fetching conversations:', error);
            showToast('Error loading conversation history', 'error');
        }
    }

    // Delete a conversation
    async function deleteConversation(sessionID) {
        try {
            // Show loading overlay during deletion
            showLoadingOverlay();
            
            const response = await fetch('/api/chat/delete', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    userID: currentUserID,
                    sessionID: sessionID
                }),
            });
            
            const data = await response.json();
            
            if (response.ok && data.success) {
                // If we deleted the current conversation, create a new one
                if (sessionID === currentSessionID) {
                    // Clear the current messages
                    chatMessages.innerHTML = '';
                    
                    // Start a new conversation
                    await startOrJoinChat('');
                }
                
                // Refresh the conversations list
                await fetchUserConversations();
                
                showToast('Conversation deleted successfully', 'success');
            } else {
                throw new Error(data.error || 'Failed to delete conversation');
            }
        } catch (error) {
            console.error('Error deleting conversation:', error);
            showToast(`Error: ${error.message || 'Failed to delete conversation'}`, 'error');
        } finally {
            // Hide loading overlay
            hideLoadingOverlay();
            
            // Hide delete confirmation dialog if it's visible
            hideDeleteConfirmDialog();
        }
    }
    
    // Show delete confirmation dialog
    function showDeleteConfirmDialog(sessionID) {
        // Save the session ID to delete
        deletingSessionID = sessionID;
        
        // Create the dialog if it doesn't exist
        let dialog = document.getElementById('delete-confirm-dialog');
        
        if (!dialog) {
            dialog = document.createElement('div');
            dialog.id = 'delete-confirm-dialog';
            dialog.classList.add('delete-confirm-dialog');
            
            const content = document.createElement('div');
            content.classList.add('delete-confirm-content');
            
            content.innerHTML = `
                <h3>Delete Conversation</h3>
                <p>Are you sure you want to delete this conversation? This action cannot be undone.</p>
                <div class="delete-confirm-buttons">
                    <button id="cancel-delete-btn" class="btn btn-cancel">Cancel</button>
                    <button id="confirm-delete-btn" class="btn btn-delete">Delete</button>
                </div>
            `;
            
            dialog.appendChild(content);
            document.body.appendChild(dialog);
            
            // Add event listeners to the buttons
            document.getElementById('cancel-delete-btn').addEventListener('click', hideDeleteConfirmDialog);
            document.getElementById('confirm-delete-btn').addEventListener('click', confirmDelete);
        }
        
        // Show the dialog
        dialog.style.display = 'flex';
    }
    
    // Hide delete confirmation dialog
    function hideDeleteConfirmDialog() {
        const dialog = document.getElementById('delete-confirm-dialog');
        if (dialog) {
            dialog.style.display = 'none';
        }
        deletingSessionID = '';
    }
    
    // Confirm delete action
    function confirmDelete() {
        if (deletingSessionID) {
            deleteConversation(deletingSessionID);
        }
    }

    // Update the conversations UI in the sidebar
    function updateConversationsUI() {
        // Clear previous conversations except for "No conversations" message
        const children = Array.from(conversationsList.children);
        for (const child of children) {
            if (!child.classList.contains('no-conversations')) {
                conversationsList.removeChild(child);
            }
        }
        
        // Show or hide the "No conversations" message
        if (userConversations.length > 0) {
            noConversations.style.display = 'none';
        } else {
            noConversations.style.display = 'flex';
            return;
        }
        
        // Add conversation items to the sidebar
        userConversations.forEach(conv => {
            const convItem = document.createElement('div');
            convItem.classList.add('conversation-item');
            
            // Highlight active conversation
            if (conv.sessionID === currentSessionID) {
                convItem.classList.add('active');
            }
            
            // Create the conversation item structure with just ID and message count
            convItem.innerHTML = `
                <div class="conversation-title">${truncateText(conv.sessionID, 20)}</div>
                <div class="conversation-meta">
                    <span class="conversation-count"><i class="far fa-comments"></i> ${conv.messageCount}</span>
                </div>
            `;
            
            // Create delete button
            const deleteBtn = document.createElement('button');
            deleteBtn.classList.add('delete-conversation-btn');
            deleteBtn.innerHTML = '<i class="fas fa-trash-alt"></i>';
            deleteBtn.title = 'Delete conversation';
            deleteBtn.setAttribute('aria-label', 'Delete conversation');
            
            // Add click event to delete button - stop propagation to prevent also selecting the conversation
            deleteBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                showDeleteConfirmDialog(conv.sessionID);
            });
            
            // Add the delete button to the item
            convItem.appendChild(deleteBtn);
            
            // Add click event to switch to this conversation
            convItem.addEventListener('click', () => switchConversation(conv.sessionID));
            
            conversationsList.appendChild(convItem);
        });
    }

    // Switch to a different conversation
    async function switchConversation(sessionID) {
        if (sessionID === currentSessionID) return;
        
        // Show loading overlay during the switch
        showLoadingOverlay();
        
        currentSessionID = sessionID;
        
        try {
            // Update sidebar to highlight active conversation
            updateConversationsUI();
            
            // Load the selected conversation history
            await loadChatHistory();
            
            // Enable chat input
            messageInput.disabled = false;
            sendBtn.disabled = false;
            
            // Focus on the message input
            messageInput.focus();
        } catch (error) {
            console.error('Error switching conversations:', error);
            showToast('Error switching conversations', 'error');
        } finally {
            // Hide loading overlay
            hideLoadingOverlay();
        }
    }

    // Load existing chat history
    async function loadChatHistory() {
        try {
            const response = await fetch(`/api/chat/history?userID=${currentUserID}&sessionID=${currentSessionID}`);
            
            if (response.ok) {
                const data = await response.json();
                
                chatMessages.innerHTML = '';
                
                if (data.messages && data.messages.length > 0) {
                    data.messages.forEach(msg => {
                        const messageType = msg.type === 'human' ? 'user' : 'ai';
                        appendMessage(messageType, msg.content);
                    });
                    
                    // Scroll to the bottom
                    scrollToBottom();
                } else {
                    chatMessages.innerHTML = `
                        <div class="empty-chat-message">
                            <i class="far fa-comment-dots fa-3x"></i>
                            <p>What can I help with?</p>
                        </div>`;
                }
            } else {
                const data = await response.json();
                console.error('Failed to load chat history:', data.error);
                showToast('Failed to load chat history', 'error');
            }
        } catch (error) {
            console.error('Error loading chat history:', error);
            showToast('Error loading chat messages', 'error');
        }
    }

    // Append a message to the chat
    function appendMessage(sender, text) {
        const messageDiv = document.createElement('div');
        messageDiv.classList.add('message');
        messageDiv.classList.add(sender === 'user' ? 'user-message' : 'ai-message');
        
        const messageContent = document.createElement('div');
        messageContent.classList.add('message-content');
        
        // Process text content based on the sender type
        if (sender === 'ai') {
            // For AI messages, render as Markdown
            messageContent.innerHTML = marked.parse(text);
        } else {
            // For user messages, use plain text
            messageContent.textContent = text;
        }
        
        messageDiv.appendChild(messageContent);
        chatMessages.appendChild(messageDiv);
        
        // Add animation class
        messageDiv.classList.add('message-appear');
        
        scrollToBottom();
    }

    // Scroll chat to the bottom
    function scrollToBottom() {
        chatMessages.scrollTop = chatMessages.scrollHeight;
    }

    // Helper to truncate text with ellipsis - simplified to just handle session IDs
    function truncateText(text, maxLength) {
        if (!text) return '';
        return text.length > maxLength ? text.substring(0, maxLength) + '...' : text;
    }
    
    // Check for stored user ID and auto-login
    const storedUserID = localStorage.getItem('chatUserID');
    if (storedUserID) {
        userIDInput.value = storedUserID;
    } else {
        // Focus on the user ID input if no stored ID
        userIDInput.focus();
    }
});