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
	"time"
	
	//Import the Redis client library
	redis "github.com/redis/go-redis/v9"
)

var redisClient *redis.Client
var ctx = context.Background()

// Define API keys for different models from environment variables.
var geminiAPIKey = os.Getenv("GEMINI_API_KEY")
var llamaAPIKey = os.Getenv("LLAMA_API_KEY")
var claudeAPIKey = os.Getenv("CLAUDE_API_KEY")
var chatGPTAPIKey = os.Getenv("CHATGPT_API_KEY")

// Message represents a single turn in the conversation, used for storage and retrieval.
type Message struct {
	Role string `json:"role"` // "user", "ai", or "system"
	Text string `json:"text"`
}

// ClientRequestPayload represents the structure of the incoming request from the client,
// now including a field to specify the model.
type ClientRequestPayload struct {
	ModelName string `json:"modelName"`
	Contents []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"contents"`
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

	var aiText string
	var err error

	switch clientPayload.ModelName {
	case "gemini":
		aiText, err = callGeminiAPI(clientPayload.Contents)
	case "llama":
		aiText, err = callLlamaAPI(clientPayload.Contents)
	case "claude":
		aiText, err = callClaudeAPI(clientPayload.Contents)
	case "chatgpt":
		aiText, err = callChatGPTAPI(clientPayload.Contents)
	default:
		http.Error(w, "Invalid model name", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"text": aiText})
}

func callGeminiAPI(contents []struct {
	Role string `json:"role"`
	Text string `json:"text"`
}) (string, error) {
	if geminiAPIKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	geminiContents := make([]GeminiMessage, len(contents))
	for i, c := range contents {
		role := "user"
		if c.Role == "ai" {
			role = "model"
		}
		geminiContents[i] = GeminiMessage{
			Role: role,
			Parts: []GeminiPart{{Text: c.Text}},
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

func callLlamaAPI(contents []struct {
	Role string `json:"role"`
	Text string `json:"text"`
}) (string, error) {
	if llamaAPIKey == "" {
		return "", fmt.Errorf("LLAMA_API_KEY environment variable not set")
	}

	llamaMessages := make([]PerplexityMessage, len(contents))
	for i, c := range contents {
		role := "user"
		if c.Role == "ai" {
			role = "assistant"
		}
		llamaMessages[i] = PerplexityMessage{
			Role:    role,
			Content: c.Text,
		}
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

func callClaudeAPI(contents []struct {
	Role string `json:"role"`
	Text string `json:"text"`
}) (string, error) {
	if claudeAPIKey == "" {
		return "", fmt.Errorf("CLAUDE_API_KEY environment variable not set")
	}

	claudeMessages := make([]AnthropicMessage, len(contents))
	for i, c := range contents {
		role := "user"
		if c.Role == "ai" {
			role = "assistant"
		}
		claudeMessages[i] = AnthropicMessage{
			Role:    role,
			Content: c.Text,
		}
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

func callChatGPTAPI(contents []struct {
	Role string `json:"role"`
	Text string `json:"text"`
}) (string, error) {
	if chatGPTAPIKey == "" {
		return "", fmt.Errorf("CHATGPT_API_KEY environment variable not set")
	}

	openaiMessages := make([]OpenaiMessage, len(contents))
	for i, c := range contents {
		role := "user"
		if c.Role == "ai" {
			role = "assistant"
		}
		openaiMessages[i] = OpenaiMessage{
			Role:    role,
			Content: c.Text,
		}
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

func main() {
	InitRedis() // <-- Call the initialization function here. You need to call this function early in your main()
	
	// POST handler for sending new messages
	http.HandleFunc("/chat", chatHandler)
	
	// GET handler for retrieving history on refresh ---
    http.HandleFunc("/chat/history", getChatHistoryHandler)
    
	port := "8080"
	log.Printf("Server started on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
