package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	
	//Import the Redis client library
	redis "github.com/redis/go-redis/v9"
	
	//Import the Qdrant client library
	"github.com/qdrant/go-client/qdrant"
)

var redisClient *redis.Client
var ctx = context.Background()

// Near your other global variables (redisClient, etc.)
var embedder Embedder

// Define API keys for different models from environment variables.
var geminiAPIKey = os.Getenv("GEMINI_API_KEY")
var llamaAPIKey = os.Getenv("LLAMA_API_KEY")
var claudeAPIKey = os.Getenv("CLAUDE_API_KEY")
var chatGPTAPIKey = os.Getenv("CHATGPT_API_KEY")

// ClientRequestPayload represents the structure of the incoming request from the client,
// now including a field to specify the model.
type ClientRequestPayload struct {
	SessionID string `json:"sessionId"` // <-- NEW!
	ModelName string `json:"modelName"`
	Contents []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"contents"` // This contents array now only holds the NEW user message
}

// Message represents a single turn in the conversation, used for storage and retrieval.
// We will also use the Message struct defined earlier (Step 2.3) for Redis storage
type Message struct {
	Role string `json:"role"` // "user", "ai", or "system"
	Text string `json:"text"`
}

// The Profile is the reference for the AI on user preferences.
type Profile struct {
    // 1. Fixed Core Fields (Necessary for application logic)
    SessionID string `json:"sessionId"` // Mandatory for Redis key linking
    Name      string `json:"name"`      // The user's name (high-value)
    
    // 2. Dynamic Preferences (Flexible Key-Value Store)
    // This map can hold things like "favorite_color", "cuisine_preference", 
    // "learning_goal", or any facts the AI learns.
    Preferences map[string]string `json:"preferences"` 
}

// Embedder is the contract for turning text into vectors.
type Embedder interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
}

// ---- Gemini API structs ----
type GeminiPayload struct {
	Contents         []GeminiMessage `json:"contents"`
	GenerationConfig map[string]interface{} `json:"generationConfig"`
}

type GeminiMessage struct {
	Role  string        `json:"role"`
	Parts []GeminiPart  `json:"parts"`
}

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content GeminiMessage `json:"content"`
	} `json:"candidates"`
}

// ---- OpenAI (ChatGPT) API structs ----
type OpenaiPayload struct {
	Model    string `json:"model"`
	Messages []OpenaiMessage `json:"messages"`
}

type OpenaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenaiResponse struct {
	Choices []struct {
		Message OpenaiMessage `json:"message"`
	} `json:"choices"`
}

// ---- Anthropic (Claude) API structs ----
type AnthropicPayload struct {
	Model    string `json:"model"`
	Messages []AnthropicMessage `json:"messages"`
	MaxTokens int    `json:"max_tokens"`
}

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AnthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// ---- Perplexity (Llama) API structs ----
type PerplexityPayload struct {
	Model    string `json:"model"`
	Messages []PerplexityMessage `json:"messages"`
}

type PerplexityMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PerplexityResponse struct {
	Choices []struct {
		Message PerplexityMessage `json:"message"`
	} `json:"choices"`
}

// ChunkOptions defines the parameters for our memory fragmentation
type ChunkOptions struct {
	Size    int // Target characters per chunk (e.g., 500-1000)
	Overlap int // Number of characters to carry over (e.g., 10-15%)
}

// CHAT_HISTORY_TTL is the Time-To-Live (expiry) for the Redis key (e.g., 24 hours)
const CHAT_HISTORY_TTL = 24 * time.Hour 

// InitRedis connects to Redis and checks the connection.
func InitRedis() {
    redisAddr := os.Getenv("REDIS_ADDR")
    if redisAddr == "" {
        // We will default to skipping Redis if the variable isn't set
        // This makes the service flexible in different environments.
        fmt.Println("REDIS_ADDR not set. Running in stateless mode.")
        return
    }

    // 1. Create a new client instance
    redisClient = redis.NewClient(&redis.Options{
        Addr:     redisAddr,
        Password: "", // No password set in our docker-compose for now
        DB:       0,  // Use default DB
    })

    // 2. Test the connection with PING
    pingResult, err := redisClient.Ping(ctx).Result()
    if err != nil {
        fmt.Printf("❌ Failed to connect to Redis at %s: %v\n", redisAddr, err)
        // Crash the application if connection is essential (Best Practice for production)
        os.Exit(1) 
    }

    fmt.Printf("✅ Successfully connected to Redis: %s\n", pingResult)
}

// getHistoryFromRedis fetches the chat history for a given session ID.
func getHistoryFromRedis(sessionId string) ([]Message, error) {
	if redisClient == nil {
		// Fallback for stateless mode (should not happen if InitRedis succeeded)
		return nil, fmt.Errorf("Redis client is not initialized")
	}

	historyJSON, err := redisClient.Get(ctx, sessionId).Result()
	if err == redis.Nil {
		// Key not found (new session), return empty history
		return []Message{}, nil 
	}
	if err != nil {
		// Redis connection error
		return nil, fmt.Errorf("redis error retrieving history: %w", err)
	}

	var history []Message
	if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
		return nil, fmt.Errorf("error unmarshaling history JSON: %w", err)
	}
	return history, nil
}

// saveHistoryToRedis saves the updated chat history for a given session ID, setting a TTL.
func saveHistoryToRedis(sessionId string, history []Message) error {
	if redisClient == nil {
		return fmt.Errorf("Redis client is not initialized")
	}

	historyJSON, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("error marshaling history: %w", err)
	}

	// Save the JSON string to Redis with a 24-hour TTL
	err = redisClient.Set(ctx, sessionId, historyJSON, CHAT_HISTORY_TTL).Err()
	if err != nil {
		return fmt.Errorf("redis error saving history: %w", err)
	}
	return nil
}

// getProfileFromRedis fetches the user profile for a given session ID.
func getProfileFromRedis(sessionId string) (Profile, error) {
    if redisClient == nil {
        return Profile{}, fmt.Errorf("Redis client is not initialized")
    }

    // Use a unique key for the profile
    profileKey := "profile:" + sessionId 

    profileJSON, err := redisClient.Get(ctx, profileKey).Result()
    if err == redis.Nil {
        // Profile not found (new user), return an empty profile struct
        return Profile{
            SessionID: sessionId,
            Preferences: make(map[string]string), // Initialize the map to avoid nil errors
        }, nil 
    }
    if err != nil {
        // Redis connection error
        return Profile{}, fmt.Errorf("redis error retrieving profile: %w", err)
    }

    var profile Profile
    if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
        return Profile{}, fmt.Errorf("error unmarshaling profile JSON: %w", err)
    }
    
    // Ensure the Preferences map is initialized even if the JSON didn't include it
    if profile.Preferences == nil {
        profile.Preferences = make(map[string]string)
    }

    return profile, nil
}

// PROFILE_TTL defines how long the long-term memory lasts (e.g., 30 days)
const PROFILE_TTL = 30 * 24 * time.Hour

func saveProfileToRedis(sessionId string, profile Profile) error {
	if redisClient == nil {
		return fmt.Errorf("Redis client is not initialized")
	}

	profileKey := "profile:" + sessionId

	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("error marshaling profile: %w", err)
	}

	// Save to Redis with a 30-day expiration
	err = redisClient.Set(ctx, profileKey, profileJSON, PROFILE_TTL).Err()
	if err != nil {
		return fmt.Errorf("redis error saving profile: %w", err)
	}
	return nil
}

// chatHandler acts as a router to the correct LLM API.
func chatHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
		return
	}

	var clientPayload ClientRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&clientPayload); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	
	// Check for required fields
    if clientPayload.SessionID == "" || len(clientPayload.Contents) == 0 {
        http.Error(w, "Missing sessionId or message content", http.StatusBadRequest)
        return
    }
    
    // 1. Retrieve History (Short-Term Memory)
	history, err := getHistoryFromRedis(clientPayload.SessionID)
	if err != nil {
		log.Printf("Error in getHistoryFromRedis: %v", err)
		http.Error(w, "Internal server error retrieving history", http.StatusInternalServerError)
		return
	}
	
	// 2. Retrieve Profile (Long-Term Memory) - NEW!
	profile, err := getProfileFromRedis(clientPayload.SessionID)
	if err != nil {
		log.Printf("Error retrieving profile: %v", err)
		// We continue even if profile fails; we'll just use a generic prompt.
	}

	// 3. Build the Personalized System Prompt
	systemPromptText := "You are a helpful and friendly AI assistant. Keep your answers concise."
	if profile.Name != "" {
		systemPromptText = fmt.Sprintf("You are an assistant for %s. Address them by name occasionally. ", profile.Name)
	}
	
	// Add preferences to the instructions
	if len(profile.Preferences) > 0 {
		systemPromptText += "Here is what you know about the user: "
		for key, value := range profile.Preferences {
			systemPromptText += fmt.Sprintf("- %s: %s. ", key, value)
		}
	}
	systemPromptText += "Keep your responses concise and tailored to these preferences."
	
	// 4. Construct the Full Context for the LLM API
	// We create a temporary slice for the API call that starts with our Dynamic System Prompt
	fullContext := []Message{
		{Role: "system", Text: systemPromptText},
	}
	
	// Append the existing history and the brand-new user message
	history = append(fullContext, history...)
	
	// 3. System Prompt (Handle new session context)
    // If the history is empty, prepend the system prompt.
    if len(history) == 0 {
        // NOTE: We will hardcode the system prompt for now, 
        // but this will be moved to a config variable later.
        systemPromptText := Message{
            Role: "system",
            Text: "You are a helpful and friendly AI assistant. Keep your answers concise.",
        }
        history = append(history, systemPromptText)
    }
    
    // 4. Append the NEW User Message to the full history
	// The clientPayload.Contents[0] is the new message sent from the FE.
    newMessage := clientPayload.Contents[0]
	history = append(history, Message{
        Role: newMessage.Role,
        Text: newMessage.Text,
    })
    
    // 5. Prepare Full Context for LLM Call
	// We pass the full, assembled 'history' array to the LLM functions.
	// NOTE: The LLM API functions must be updated in Step 4 below to accept the []Message type.
	var aiText string
	//var err error

	switch clientPayload.ModelName {
	case "gemini":
		aiText, err = callGeminiAPI(history)
	case "llama":
		aiText, err = callLlamaAPI(history)
	case "claude":
		aiText, err = callClaudeAPI(history)
	case "chatgpt":
		aiText, err = callChatGPTAPI(history)
	default:
		http.Error(w, "Invalid model name", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// 6. Append the AI Response to the history
    aiMessage := Message{
        Role: "ai",
        Text: aiText,
    }
    history = append(history, aiMessage)

	// 6. Persistence: Save only the conversation turns (User + AI) to Redis
	// We do NOT save the system prompt here, as we regenerate it every time.
	updatedHistory := append(history, newMessage, Message{Role: "ai", Text: aiText})
	// 7. Save the Full Updated History back to Redis
	if err := saveHistoryToRedis(clientPayload.SessionID, updatedHistory); err != nil {
		log.Printf("Error in saveHistoryToRedis: %v", err)
        // Log the error but don't necessarily fail the response, as the user got the answer.
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"text": aiText})
}

//func callGeminiAPI(contents []struct {
//	Role string `json:"role"`
//	Text string `json:"text"`
//}) (string, error) {
func callGeminiAPI(contents []Message) (string, error) { // NEW
	if geminiAPIKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	geminiContents := make([]GeminiMessage, 0, len(contents))
	//for i, c := range contents {
	for _, c := range contents {
		role := "" // Initialize role to an empty string
		
		// 1. Handle Role Mapping for Gemini API
		switch c.Role {
		case "user":
			role = "user"
		case "ai":
			role = "model"
        case "system":
            // 2. IMPORTANT FIX: Map the system role to "user" for now, 
            // so the LLM processes it as a context-setting instruction.
            // This is temporary until you adopt the proper systemInstruction field.
            role = "user" 
		default:
            // Skip any unknown roles
            // If the role is unexpected (e.g., a typo), we skip it entirely
            log.Printf("Warning: Skipping message with invalid role: %s", c.Role)
            continue
		}
	//	role := "user"
	//	if c.Role == "ai" {
	//		role = "model"
	//	}

	//	geminiContents[i] = GeminiMessage{
	//		Role: role,
	//		Parts: []GeminiPart{{Text: c.Text}},
	//	}
		// 3. Create the GeminiMessage using the Message struct fields (c.Text)
		if role != "" {
			geminiContents = append(geminiContents, GeminiMessage{
				Role: role,
				Parts: []GeminiPart{{Text: c.Text}}, // c.Text comes from the Message struct
			})
		}
	}

	payload := GeminiPayload{
		Contents: geminiContents,
		GenerationConfig: map[string]interface{}{
			"temperature": 0.7,
			"topP": 0.95,
			"topK": 40,
			"maxOutputTokens": 1024,
		},
	}

	jsonPayload, _ := json.Marshal(payload)
	apiUrl := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=%s", geminiAPIKey)
	resp, err := makeAPIRequest(apiUrl, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error parsing Gemini response: %w", err)
	}

	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		return result.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("unexpected Gemini response structure")
}

//func callLlamaAPI(contents []struct {
//	Role string `json:"role"`
//	Text string `json:"text"`
//}) (string, error) {
func callLlamaAPI(contents []Message) (string, error) { // NEW
	if llamaAPIKey == "" {
		return "", fmt.Errorf("LLAMA_API_KEY environment variable not set")
	}

	llamaMessages := make([]PerplexityMessage, len(contents))
	
	//for i, c := range contents {
	//	role := "user"
	//	if c.Role == "ai" {
	//		role = "assistant"
	//	}
	//	llamaMessages[i] = PerplexityMessage{
	//		Role:    role,
	//		Content: c.Text,
	//	}
	//}
	//for i, c := range contents {
	for _, c := range contents {
		role := "" // Initialize role to an empty string
		
		// 1. Handle Role Mapping for Gemini API
		switch c.Role {
		case "user":
			role = "user"
		case "ai":
			role = "assistant"
        case "system":
            // 2. IMPORTANT FIX: Map the system role to "user" for now, 
            // so the LLM processes it as a context-setting instruction.
            // This is temporary until you adopt the proper systemInstruction field.
            role = "user" 
		default:
            // Skip any unknown roles
            continue
		}
		// 3. Create the GeminiMessage using the Message struct fields (c.Text)
		llamaMessages = append(llamaMessages, PerplexityMessage{
			Role: role,
			Content: c.Text, // c.Text comes from the Message struct
		})
	}

	payload := PerplexityPayload{
		Model: "llama-3-sonar-small-32k-online",
		Messages: llamaMessages,
	}

	jsonPayload, _ := json.Marshal(payload)
	apiUrl := "https://api.perplexity.ai/chat/completions"
	resp, err := makeAPIRequestWithAuth(apiUrl, "Bearer "+llamaAPIKey, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result PerplexityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error parsing Llama response: %w", err)
	}

	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("unexpected Llama response structure")
}

//func callClaudeAPI(contents []struct {
//	Role string `json:"role"`
//	Text string `json:"text"`
//}) (string, error) {
func callClaudeAPI(contents []Message) (string, error) { // NEW
	if claudeAPIKey == "" {
		return "", fmt.Errorf("CLAUDE_API_KEY environment variable not set")
	}

	claudeMessages := make([]AnthropicMessage, len(contents))
	
	//for i, c := range contents {
	//	role := "user"
	//	if c.Role == "ai" {
	//		role = "assistant"
	//	}
	//	claudeMessages[i] = AnthropicMessage{
	//		Role:    role,
	//		Content: c.Text,
	//	}
	//}
	for _, c := range contents {
		role := "" // Initialize role to an empty string
		
		// 1. Handle Role Mapping for Gemini API
		switch c.Role {
		case "user":
			role = "user"
		case "ai":
			role = "asssitant"
        case "system":
            // 2. IMPORTANT FIX: Map the system role to "user" for now, 
            // so the LLM processes it as a context-setting instruction.
            // This is temporary until you adopt the proper systemInstruction field.
            role = "user" 
		default:
            // Skip any unknown roles
            continue
		}
		// 3. Create the GeminiMessage using the Message struct fields (c.Text)
		claudeMessages = append(claudeMessages, AnthropicMessage{
			Role: role,
			Content: c.Text, // c.Text comes from the Message struct
		})
	}

	payload := AnthropicPayload{
		Model:    "claude-3-opus-20240229",
		Messages: claudeMessages,
		MaxTokens: 1024,
	}

	jsonPayload, _ := json.Marshal(payload)
	apiUrl := "https://api.anthropic.com/v1/messages"
	resp, err := makeAPIRequestWithAuthAndHeader(apiUrl, "x-api-key", claudeAPIKey, "anthropic-version", "2023-06-01", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error parsing Claude response: %w", err)
	}

	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}

	return "", fmt.Errorf("unexpected Claude response structure")
}

//func callChatGPTAPI(contents []struct {
//	Role string `json:"role"`
//	Text string `json:"text"`
//}) (string, error) {
func callChatGPTAPI(contents []Message) (string, error) { // NEW
	if chatGPTAPIKey == "" {
		return "", fmt.Errorf("CHATGPT_API_KEY environment variable not set")
	}

	openaiMessages := make([]OpenaiMessage, len(contents))
	
	//for i, c := range contents {
	//	role := "user"
	//	if c.Role == "ai" {
	//		role = "assistant"
	//	}
	//	openaiMessages[i] = OpenaiMessage{
	//		Role:    role,
	//		Content: c.Text,
	//	}
	//}
	for _, c := range contents {
		role := "" // Initialize role to an empty string
		
		// 1. Handle Role Mapping for Gemini API
		switch c.Role {
		case "user":
			role = "user"
		case "ai":
			role = "assistant"
        case "system":
            // 2. IMPORTANT FIX: Map the system role to "user" for now, 
            // so the LLM processes it as a context-setting instruction.
            // This is temporary until you adopt the proper systemInstruction field.
            role = "user" 
		default:
            // Skip any unknown roles
            continue
		}
		// 3. Create the GeminiMessage using the Message struct fields (c.Text)
		openaiMessages = append(openaiMessages, OpenaiMessage{
			Role: role,
			Content: c.Text, // c.Text comes from the Message struct
		})
	}

	payload := OpenaiPayload{
		Model:    "gpt-4o",
		Messages: openaiMessages,
	}

	jsonPayload, _ := json.Marshal(payload)
	apiUrl := "https://api.openai.com/v1/chat/completions"
	resp, err := makeAPIRequestWithAuth(apiUrl, "Bearer "+chatGPTAPIKey, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result OpenaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("error parsing ChatGPT response: %w", err)
	}

	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("unexpected ChatGPT response structure")
}

func makeAPIRequest(url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making API request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API returned status code %d: %s", resp.StatusCode, string(respBody))
	}
	return resp, nil
}

func makeAPIRequestWithAuth(url, authHeader string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making API request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API returned status code %d: %s", resp.StatusCode, string(respBody))
	}
	return resp, nil
}

func makeAPIRequestWithAuthAndHeader(url, authHeaderName, authHeaderValue, otherHeaderName, otherHeaderValue string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authHeaderName, authHeaderValue)
	req.Header.Set(otherHeaderName, otherHeaderValue)
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making API request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API returned status code %d: %s", resp.StatusCode, string(respBody))
	}
	return resp, nil
}

// getChatHistoryHandler retrieves the full conversation history for a given session ID.
func getChatHistoryHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

    if r.Method == "OPTIONS" {
        w.WriteHeader(http.StatusOK)
        return
    }

    if r.Method != "GET" {
        http.Error(w, "Only GET requests are allowed", http.StatusMethodNotAllowed)
        return
    }

    // 1. Get Session ID from query parameters
    sessionId := r.URL.Query().Get("sessionId")
    if sessionId == "" {
        http.Error(w, "Missing sessionId query parameter", http.StatusBadRequest)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    
    // 2. Retrieve history JSON string from Redis
    historyJSON, err := redisClient.Get(ctx, sessionId).Result()
    
    if err == redis.Nil {
        // 3a. Key not found (new session), return an empty array []
        json.NewEncoder(w).Encode([]Message{}) 
        return
    } else if err != nil {
        log.Printf("Redis error retrieving history for %s: %v", sessionId, err)
        http.Error(w, "Internal server error retrieving history", http.StatusInternalServerError)
        return
    }

    // 3b. Key found, return the history JSON directly
    // Note: We don't unmarshal/re-marshal here for efficiency; we just pipe the JSON string
    w.Write([]byte(historyJSON))
}

func profileHandler(w http.ResponseWriter, r *http.Request) {
	// Enable CORS (Standard for your existing handlers)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
		return
	}

	var profile Profile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		http.Error(w, "Invalid profile payload", http.StatusBadRequest)
		return
	}

	if profile.SessionID == "" {
		http.Error(w, "Missing sessionId in profile", http.StatusBadRequest)
		return
	}

	// Save the updated profile to Redis
	if err := saveProfileToRedis(profile.SessionID, profile); err != nil {
		log.Printf("Error saving profile: %v", err)
		http.Error(w, "Failed to save profile", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "profile updated"})
}

type GeminiEmbedder struct {
	APIKey string
}

// These structs match the Google AI API schema
type geminiEmbedRequest struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
	// Use Matryoshka learning to truncate to 768 dimensions
	OutputDimensionality int `json:"outputDimensionality"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

func (g *GeminiEmbedder) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Stable v1 endpoint for gemini-embedding-001
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:embedContent?key=%s", g.APIKey)

	// Prepare the payload
	reqBody := geminiEmbedRequest{
		OutputDimensionality: 768, // Forces the 3072-dim model to return a optimized 768-dim vector
	}
	reqBody.Content.Parts = []struct {
		Text string `json:"text"`
	}{{Text: text}}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Send it
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini embedding error: status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result geminiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Embedding.Values, nil
}

func SetupCollection(ctx context.Context, client *qdrant.Client, collectionName string) error {
	return client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     768, // Matches our Gemini outputDimensionality
			Distance: qdrant.Distance_Cosine,
		}),
	})
}

func UpsertMemory(ctx context.Context, client *qdrant.Client, collection string, vector []float32, rawText string, advisorName string) error {
	// Generate a stable ID (using a UUID is standard)
	pointID := qdrant.NewIDUUID(generateUUID()) 

	// The "Point" structure
	point := &qdrant.PointStruct{
		Id:     pointID,
		Vectors: qdrant.NewVectors(vector...),
		Payload: qdrant.NewValueMap(map[string]any{
			"content":  rawText,     // THE GOLDEN RULE: Always keep the source
			"advisor":  advisorName, // Filter by Gandalf, Vader, etc.
			"created":  time.Now().Unix(),
		}),
	}

	_, err := client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collection,
		Points:         []*qdrant.PointStruct{point},
	})
	return err
}

func InitializeMemory(ctx context.Context, host string) (*qdrant.Client, error) {
    client, err := qdrant.NewClient(&qdrant.Config{
        Host: host,
        Port: 6334, // gRPC port
    })
    if err != nil {
        return nil, err
    }

    // Ensure the collection exists
    collectionName := "advisor_memories"
    exists, _ := client.HasCollection(ctx, collectionName)
    if !exists {
        err = client.CreateCollection(ctx, &qdrant.CreateCollection{
            CollectionName: collectionName,
            VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
                Size:     768, 
                Distance: qdrant.Distance_Cosine,
            }),
        })
    }

    return client, err
}

func SearchMemory(ctx context.Context, qClient *qdrant.Client, embedder *GeminiEmbedder, query string, advisorFilter string) ([]string, error) {
	// 1. Convert the Question into a Vector
	queryVector, err := embedder.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// 2. Build the Search Request
	searchQuery := &qdrant.QueryPoints{
		CollectionName: "advisor_memories",
		Query:          qdrant.NewQuery(queryVector...),
		Limit:          qdrant.Ptr(uint64(3)), // Return top 3 matches
		WithPayload:    qdrant.NewWithPayload(true),
	}

	// 3. Optional: Filter by Advisor (Darth Vader shouldn't use Gandalf's notes)
	if advisorFilter != "" {
		searchQuery.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewFieldCondition("advisor", qdrant.NewMatchText(advisorFilter)),
			},
		}
	}

	// 4. Execute the Search
	result, err := qClient.Query(ctx, searchQuery)
	if err != nil {
		return nil, fmt.Errorf("qdrant search failed: %w", err)
	}

	// 5. Extract the "Golden Rule" Raw Text from the Payload
	var memories []string
	for _, point := range result {
		if content, ok := point.Payload["content"]; ok {
			memories = append(memories, content.GetStringValue())
		}
	}

	return memories, nil
}

func RecursiveSplit(text string, opts ChunkOptions) []string {
	// 1. If text is already small enough, we're done
	if len(text) <= opts.Size {
		return []string{text}
	}

	// 2. Define our "Polite" separators from most significant to least
	separators := []string{"\n\n", "\n", " ", ""}
	var chosenSeparator string
	
	for _, sep := range separators {
		if strings.Contains(text, sep) {
			chosenSeparator = sep
			break
		}
	}

	// 3. Split the text
	parts := strings.Split(text, chosenSeparator)
	var chunks []string
	var currentChunk strings.Builder

	for _, part := range parts {
		// If adding this part exceeds the limit, save the current chunk
		if currentChunk.Len()+len(part)+len(chosenSeparator) > opts.Size {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				
				// Handle Overlap: Start the next chunk with the tail of the previous one
				overlapStart := currentChunk.Len() - opts.Overlap
				if overlapStart < 0 { overlapStart = 0 }
				overlapText := currentChunk.String()[overlapStart:]
				
				currentChunk.Reset()
				currentChunk.WriteString(overlapText)
			}
		}
		
		if currentChunk.Len() > 0 && chosenSeparator != "" {
			currentChunk.WriteString(chosenSeparator)
		}
		currentChunk.WriteString(part)
	}

	// Add the final remaining piece
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

func main() {
	InitRedis() // <-- Call the initialization function here. You need to call this function early in your main()
	
	// Vector DB start
	apiKey := geminiAPIKey
    if apiKey == "" {
        log.Fatal("GEMINI_API_KEY must be set")
    }

    // Initialize our specific implementation for vector db embedder
    embedder = &GeminiEmbedder{APIKey: apiKey}
    
    ctx := context.Background()
    
    // 1. Setup - Call the function from here
    qClient, err := InitializeMemory(ctx, "localhost")
    if err != nil {
        log.Fatalf("Failed to init Qdrant: %v", err)
    }

    // 2. Business Logic (Task 4.3 - Ingest something)
    // ... logic to call your embedder and then UpsertMemory ...

    // 3. Business Logic (Task 4.4 - Search something)
    // ... logic to call SearchMemory ...
	
	// POST handler for sending new messages
	http.HandleFunc("/chat", chatHandler)
	
	// GET handler for retrieving history on refresh ---
    http.HandleFunc("/chat/history", getChatHistoryHandler)

    // handler for setting and retrieving profile ---
    http.HandleFunc("/profile", profileHandler)
    
	port := "8080"
	log.Printf("Server started on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
