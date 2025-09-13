const userInput = document.getElementById('user-input');
const sendButton = document.getElementById('send-button');
const chatHistoryDiv = document.getElementById('chat-history');
let chatHistory = []; // Stores the conversation for context

// Function to add a message to the chat history display
function addMessage(text, sender) {
	const messageDiv = document.createElement('div');
	messageDiv.classList.add('message');
	
	if (sender === 'user') {
		messageDiv.classList.add('user-message');
	} else {
		messageDiv.classList.add('ai-message');
	}

	messageDiv.textContent = text;
	chatHistoryDiv.appendChild(messageDiv);
	chatHistoryDiv.scrollTop = chatHistoryDiv.scrollHeight; // Scroll to bottom
}

// Function to display a loading indicator
function showLoading() {
	const loadingDiv = document.createElement('div');
	loadingDiv.id = 'loading-indicator';
	loadingDiv.classList.add('loading-indicator');
	loadingDiv.textContent = 'AI is thinking...';
	chatHistoryDiv.appendChild(loadingDiv);
	chatHistoryDiv.scrollTop = chatHistoryDiv.scrollHeight;
}

// Function to remove the loading indicator
function hideLoading() {
	const loadingDiv = document.getElementById('loading-indicator');

	if (loadingDiv) {
		loadingDiv.remove();
	}
}

// Function to send message to LLM
async function sendMessage() {
	const userText = userInput.value.trim();

	if (userText === '') return;
		addMessage(userText, 'user');
		userInput.value = ''; // Clear input field
		// Add user message to chat history for LLM context
		chatHistory.push({ role: "user", parts: [{ text: userText }] });
		showLoading(); // Show loading indicator
		try {
			const payload = {
				modelName: "gemini",
				contents: chatHistory,
				generationConfig: {
					temperature: 0.7, // Adjust for creativity vs. focus
					topP: 0.95,
					topK: 40,
					maxOutputTokens: 1024
				}
			};

			const response = await fetch('http://localhost:8080/chat', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(payload),
			});

			if (!response.ok) {
				const errorData = await response.json();
				throw new Error(`Server error: ${response.status} - ${errorData.text}`);
			}

			const result = await response.json();
			hideLoading();
			addMessage(result.text, 'ai');
			chatHistory.push({ role: 'ai', text: result.text });
		} catch (error) {
			hideLoading(); // Hide loading indicator even on error
			addMessage("An error occurred: " + error.message, 'ai');
			console.error("Error communicating with backend:", error);
		}
	}

	// Event listeners
	sendButton.addEventListener('click', sendMessage);
	userInput.addEventListener('keypress', function(event) {

	if (event.key === 'Enter') {
		sendMessage();
	}
});
