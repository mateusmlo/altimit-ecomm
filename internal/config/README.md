# Config Package

This package provides centralized configuration management for the application using [Viper](https://github.com/spf13/viper).

## Features

- Reads configuration from `.env` file
- Automatic environment variable binding
- Comprehensive validation of all required fields
- Type-safe configuration struct
- Helper methods for common connection strings

## Usage

### Basic Usage

```go
package main

import (
    "log"
    "github.com/mateusmlo/altimit-ecomm/internal/config"
)

func main() {
    // Load configuration
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    // Access configuration values
    log.Printf("Kafka brokers: %v", cfg.Kafka.Brokers)
    log.Printf("Region: %s", cfg.Region)
}
```

### Accessing Configuration Values

```go
// Kafka configuration
brokers := cfg.Kafka.Brokers

// PostgreSQL configuration
dbUser := cfg.Postgres.User
dbPass := cfg.Postgres.Password
dbHost := cfg.Postgres.Host
dbPort := cfg.Postgres.Port
dbName := cfg.Postgres.DB

// Redis configuration
redisHost := cfg.Redis.Host
redisPort := cfg.Redis.Port

// Topics
ordersTopicName := cfg.Topics.Commands.Orders
inventoryCommandsTopic := cfg.Topics.Commands.Inventory
inventoryRepliesTopic := cfg.Topics.Replies.Inventory

// Consumer groups
orchestratorGroup := cfg.ConsumerGroups.SagaOrchestrator

// Region
region := cfg.Region
```

### Helper Methods

The config package provides convenient helper methods:

```go
// Get PostgreSQL connection string
connStr := cfg.GetPostgresConnectionString()
// Returns: "host=localhost port=5432 user=user password=pass dbname=db sslmode=disable"

// Get Redis address
redisAddr := cfg.GetRedisAddress()
// Returns: "localhost:6379"
```

## Configuration Structure

The configuration is organized into the following sections:

### Kafka Configuration
- `Brokers`: List of Kafka broker addresses

### PostgreSQL Configuration
- `User`: Database user
- `Password`: Database password
- `DB`: Database name
- `Host`: Database host
- `Port`: Database port

### Redis Configuration
- `Host`: Redis host
- `Port`: Redis port

### Topics Configuration
- `Commands`: Command topic names (Orders, Inventory, Payment, Notification)
- `Replies`: Reply topic names (Inventory, Payment, Notification)
- `DLQ`: Dead letter queue topic names

### Consumer Groups
- `SagaOrchestrator`: Saga orchestrator consumer group
- `InventoryService`: Inventory service consumer group
- `PaymentService`: Payment service consumer group
- `NotificationService`: Notification service consumer group

### Other
- `Region`: Application region

## Validation

All required fields are validated when the configuration is loaded. If any required field is missing or invalid, `Load()` will return an error. This ensures the application fails fast with a clear error message rather than encountering issues at runtime.

## Environment Variables

See `.env.example` for a complete list of required environment variables.

## Testing

Run the tests:

```bash
go test ./internal/config
```
