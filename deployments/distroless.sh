#!/bin/bash
set -e

# --- Build Stage ---
# Create a temporary container for building
buildah from --name builder-container golang:1.23.7-alpine3.21

# Set up the working directory
buildah config --workingdir /app builder-container

# Copy source code
buildah copy builder-container . /app/

# Build the application for ARM64
buildah run builder-container sh -c "GOOS=linux GOARCH=arm64 go mod tidy"
buildah run builder-container sh -c "GOOS=linux GOARCH=arm64 go build -v -o /go/bin/app main.go"

# --- Final Stage with Distroless ---
# Create the final container with ARM64 distroless base
buildah from --name final-container --arch arm64 gcr.io/distroless/static

# Set up the working directory
buildah config --workingdir /app final-container

# Copy binary from builder
buildah copy --from builder-container final-container /go/bin/app /app/app

# Copy .env file
buildah copy --from builder-container final-container /app/.env /app/.env

# Set user
buildah config --user 65534 final-container

# Set command (replacing the shell script with direct command)
buildah config --cmd '["/app/app", "migrate"]' final-container
buildah config --entrypoint '["/app/app", "server"]' final-container

# Commit the final image
buildah commit final-container my-registry.io/myproject/myapp:arm64

# Clean up
buildah rm builder-container final-container

echo "ARM64 image built successfully: my-registry.io/myproject/myapp:arm64"
