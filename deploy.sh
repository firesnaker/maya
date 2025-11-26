#!/bin/bash

# --- 1. Load Environment Variables ---
# Instead of 'export KEY=VALUE', we'll read them from a local file.
# Create a file named '.env' in your root directory and put your variables there:
# e.g., .env content:
# API_KEY=YOUR_SECRET_VALUE
if [ -f .env ]; then
    echo "Loading environment variables from .env file..."
    export $(cat .env | xargs)
else
    echo "Warning: .env file not found. Ensure API_KEY is set."
fi

# Exit on any error
set -e

# --- 2. Run Docker Compose ---
echo "Building and starting Docker Compose services..."

# The 'up' command does the following:
# 1. Builds the Docker images defined in `backend` and `frontend` services.
# 2. Creates a network and starts the containers.
# 3. Passes the environment variables (like API_KEY) to the services.
docker compose up --build -d

echo ""
echo "Deployment Complete! 🎉"
echo "Ollama is running on: http://ollama:11434"
echo "Backend is running on: http://localhost:8080"
echo "Frontend is running on: http://localhost:3000"
echo "To stop and remove containers, run: docker compose down"
