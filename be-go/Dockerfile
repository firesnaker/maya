# Stage 1: Build the Go binary
FROM golang:1.24-alpine AS builder

WORKDIR /app
# Copy only the necessary files for building
#uncomment this when I have go.sum COPY go.mod go.sum ./ 
COPY go.mod ./ 
RUN go mod download
COPY . .

# Build the application
# We use CGO_ENABLED=0 to create a statically linked binary (better for alpine/smaller image)
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /go-backend main.go

# Stage 2: Create a minimal final image
FROM alpine:latest

# Set the working directory
WORKDIR /root/
# Copy the built binary from the builder stage
COPY --from=builder /go-backend .

# Expose the port your Go app listens on (e.g., 8080)
EXPOSE 8080

# Command to run the executable
CMD ["./go-backend"]
