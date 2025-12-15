# Dendrite Embedded

A package for embedding a Matrix homeserver into your Go applications.

## Overview

The `embedded` package allows you to integrate a fully functional Matrix homeserver within your Go application. This is useful for applications that need to provide Matrix-based communications without requiring a separate server deployment.

## Usage

# Dendrite Embedded

A package for embedding a Matrix homeserver into your Go applications.

## Overview

The `embedded` package allows you to integrate a fully functional Matrix homeserver within your Go application. This is useful for applications that need to provide Matrix-based communications without requiring a separate server deployment, or for creating specialized Matrix servers with custom networking layers (e.g., Tor, I2P).

## Usage

### Basic Example

```go
package main

import (
    "context"
    "crypto/ed25519"
    "crypto/rand"
    "log"
    "net"
    "time"

    "github.com/element-hq/dendrite/contrib/dendrite-demo-embedded"
)

func main() {
    // Generate server keys
    _, privateKey, _ := ed25519.GenerateKey(rand.Reader)

    // Configure the server
    config := embedded.DefaultConfig()
    config.ServerName = "localhost"
    config.KeyID = "ed25519:1"
    config.PrivateKey = privateKey
    config.DatabasePath = "./dendrite.db"
    config.MediaStorePath = "./media_store"
    config.JetStreamPath = "./jetstream"

    // Create server
    server, err := embedded.NewServer(config)
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }

    // Set up the listener
    listener, err := net.Listen("tcp", "0.0.0.0:8080")
    if err != nil {
        log.Fatalf("Failed to create listener: %v", err)
    }
    
    // Start the server
    if err := server.Start(context.Background(), listener); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }

    // Wait for shutdown signal
    <-server.GetProcessContext().WaitForShutdown()
    
    // Stop gracefully
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    server.Stop(ctx)
}
```

### Using with Custom Network Layer

The embedded library is designed to work with any `net.Listener` implementation, making it easy to integrate with custom networking layers:

```go
// Example: Using with a custom listener (e.g., Tor)
listener := createCustomListener() // Returns net.Listener

server, err := embedded.NewServer(config)
if err != nil {
    log.Fatal(err)
}

if err := server.Start(context.Background(), listener); err != nil {
    log.Fatal(err)
}
```

### Using with Existing Dendrite Configuration

If you already have a Dendrite configuration file or object, you can use it directly:

```go
import (
    "github.com/element-hq/dendrite/setup"
    "github.com/element-hq/dendrite/setup/config"
)

// Parse existing config
dendriteConfig := setup.ParseFlags(true)

// Use it with embedded server
config := embedded.ServerConfig{
    RawDendriteConfig: dendriteConfig,
}

server, err := embedded.NewServer(config)
// ... rest of setup
```

## Configuration

The `ServerConfig` struct allows you to configure various aspects of the embedded server:

### Basic Identity
- `ServerName`: The Matrix server name (e.g., "example.com")
- `KeyID`: The key ID for signing (e.g., "ed25519:auto")
- `PrivateKey`: The ed25519 private key for the server

### Storage Paths
- `DatabasePath`: Path to the SQLite database file
- `MediaStorePath`: Path to store uploaded media files
- `JetStreamPath`: Path to store JetStream/NATS data

### HTTP Client
- `HTTPClient`: Custom HTTP client for outbound requests (useful for routing through Tor/I2P)

### Feature Flags
- `DisableFederation`: Disable federation with other Matrix servers
- `EnableMetrics`: Enable Prometheus metrics endpoint
- `MetricsUsername`/`MetricsPassword`: Basic auth for metrics endpoint

### Performance Settings
- `CacheMaxSize`: Maximum cache size in bytes (default: 64MB)
- `CacheMaxAge`: Maximum age for cached items (default: 1 hour)

### Advanced
- `RateLimitYAMLPath`: Path to custom rate limiting configuration
- `RawDendriteConfig`: Use a complete Dendrite configuration object

## Features

- Full Matrix API support (Client-Server and Server-Server)
- Optional federation support
- Metrics and profiling capabilities
- Configurable rate limiting
- SQLite database backend
- Media storage and serving
- MSC (Matrix Spec Change) support
- Admin endpoints

## Examples

See the following implementations for real-world examples:

- **Tor**: `contrib/dendrite-demo-tor` - Matrix server over Tor onion services
- **I2P**: `contrib/dendrite-demo-i2p` - Matrix server over I2P

## Architecture

The embedded library provides a clean separation between:

1. **Core Server Logic**: Handled by the embedded package
2. **Transport Layer**: Provided by the application (via `net.Listener`)
3. **HTTP Client**: Configurable for custom routing (e.g., through anonymity networks)

This architecture allows the same server code to work with any network transport that implements the standard Go `net.Listener` interface.

## Lifecycle Management

The embedded server provides proper lifecycle management:

1. **Initialization**: `NewServer()` creates the server but doesn't start it
2. **Startup**: `Start()` begins serving on the provided listener
3. **Runtime**: Server runs until shutdown is requested
4. **Shutdown**: `Stop()` gracefully shuts down all components

The process context returned by `GetProcessContext()` can be used to coordinate shutdown signals across your application.

## Thread Safety

The embedded server is thread-safe and uses internal locking to prevent concurrent start/stop operations. Multiple goroutines can safely call `Start()` and `Stop()`## Configuration

The `ServerConfig` struct allows you to configure various aspects of the embedded server:

- Server identity (name, keys)
- Storage paths
- Feature flags (federation, metrics, etc.)
- Performance settings

Use `DefaultConfig()` as a starting point and customize as needed.

## Features

- Full Matrix API support
- Optional federation support
- Metrics and profiling capabilities
- Configurable rate limiting