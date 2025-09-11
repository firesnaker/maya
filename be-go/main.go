package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// The API key for Gemini. This should be set as an environment variable.
var apiKey = os.Getenv("GEMINI_API_KEY")

// RequestPayload represents the structure of the incoming request from the client.
type RequestPayload struct {
	Contents []Message `json:"contents"`
}

// Message represents a single message in the chat history.
type Message struct {
	Role string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part represents a part of a message, containing the text.
type Part struct {
	Text string `json:"text"`
}

// ResponsePayload represents the structure of the response from the Gemini API.
type ResponsePayload struct {
	Candidates []Candidate `json:"candidates"`
}

// Candidate represents a single candidate response from the Gemini API.
type Candidate struct {
	Content Message `json:"content"`
}

// chatHandler handles the chat requests from the client.
func chatHandler(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers to allow requests from the HTML file.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight OPTIONS request from the browser.
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the request body.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	// Parse the incoming JSON payload.
	var clientPayload RequestPayload
	if err := json.Unmarshal(body, &clientPayload); err != nil {
		http.Error(w, "Error parsing JSON request", http.StatusBadRequest)
		return
	}

	// Ensure the API key is set.
	if apiKey == "" {
		http.Error(w, "API key not found. Please set the GEMINI_API_KEY environment variable.", http.StatusInternalServerError)
		return
	}

	// Construct the URL for the Gemini API.
	apiUrl := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=%s", apiKey)

	// Marshal the payload for the Gemini API request.
	geminiPayload, err := json.Marshal(clientPayload)
	if err != nil {
		http.Error(w, "Error marshaling Gemini payload", http.StatusInternalServerError)
		return
	}

	// Create and send the POST request to the Gemini API.
	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(geminiPayload))
	if err != nil {
		http.Error(w, "Error creating API request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error communicating with Gemini API: %v", err)
		http.Error(w, "Error communicating with Gemini API", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Read the response from the Gemini API.
	geminiBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading Gemini API response", http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Gemini API error: %s", string(geminiBody))
		http.Error(w, fmt.Sprintf("API error: %s", string(geminiBody)), resp.StatusCode)
		return
	}

	// Parse the Gemini API response.
	var geminiResponse ResponsePayload
	if err := json.Unmarshal(geminiBody, &geminiResponse); err != nil {
		http.Error(w, "Error parsing Gemini API response", http.StatusInternalServerError)
		return
	}

	// Extract the text from the response and return it to the client.
	if len(geminiResponse.Candidates) > 0 && len(geminiResponse.Candidates[0].Content.Parts) > 0 {
		aiText := geminiResponse.Candidates[0].Content.Parts[0].Text
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": aiText})
	} else {
		log.Println("Unexpected Gemini API response structure")
		http.Error(w, "Unexpected API response structure", http.StatusInternalServerError)
	}
}

func main() {
	// Register the chat handler for the /chat endpoint.
	http.HandleFunc("/chat", chatHandler)

	// Start the server on port 8080.
	port := "8080"
	log.Printf("Server started on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
