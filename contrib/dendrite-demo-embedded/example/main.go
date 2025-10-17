// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

// Package main provides a minimal example of using the embedded Dendrite library.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	embedded "github.com/element-hq/dendrite/contrib/dendrite-demo-embedded"
)

func main() {
	// Generate server keys
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate keys: %v", err)
	}

	// Configure the server with sensible defaults
	config := embedded.DefaultConfig()
	config.ServerName = "localhost"
	config.KeyID = "ed25519:1"
	config.PrivateKey = privateKey
	config.DatabasePath = "./example-dendrite.db"
	config.MediaStorePath = "./example-media"
	config.JetStreamPath = "./example-jetstream"
	config.DisableFederation = true // Disable federation for this example

	log.Println("Creating embedded Dendrite server...")

	// Create the embedded server
	server, err := embedded.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Set up a standard TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:8008")
	if err != nil {
		log.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	log.Printf("Starting server on %s", listener.Addr().String())

	// Start the server
	if err := server.Start(context.Background(), listener); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	log.Println("Server started successfully!")
	log.Printf("Matrix server is available at http://%s", listener.Addr().String())
	log.Println("Press Ctrl+C to stop")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("Received shutdown signal")
	case <-server.GetProcessContext().WaitForShutdown():
		log.Println("Server initiated shutdown")
	}

	// Graceful shutdown with timeout
	log.Println("Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Server stopped")
}
