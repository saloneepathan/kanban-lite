# syntax=docker/dockerfile:1

# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder
WORKDIR /app

# Copy go.mod and go.sum first for dependency caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the binary
RUN go build -o kanban-lite main.go

# Stage 2: Create a minimal image
FROM alpine:3.20
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/kanban-lite .

# Expose the server port
EXPOSE 8080

# Run the application
CMD ["./kanban-lite"]
