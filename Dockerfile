# Use the official Golang image for building the app
FROM golang:1.20-alpine AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the Go app
RUN go build -o webhook-server ./cmd

# Final stage: a minimal image
FROM alpine:latest

# Create app directory
WORKDIR /root/

# Copy the compiled Go binary from the builder stage
COPY --from=builder /app/webhook-server .

# Expose port 8080 (same as your app)
EXPOSE 8080

# Run the executable
CMD ["./webhook-server"]