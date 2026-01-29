package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set up test environment variables
	envVars := map[string]string{
		"KAFKA_BROKERS":               "localhost:9092",
		"KAFKA_MAX_REQ_RETRIES":       "5",
		"KAFKA_MAX_RECORD_RETRIES":    "10",
		"POSTGRES_USER":               "test_user",
		"POSTGRES_PASSWORD":           "test_pass",
		"POSTGRES_DB":                 "test_db",
		"POSTGRES_HOST":               "localhost",
		"POSTGRES_PORT":               "5432",
		"REDIS_HOST":                  "localhost",
		"REDIS_PORT":                  "6379",
		"ORDERS_TOPIC":                "orders",
		"INVENTORY_COMMANDS_TOPIC":    "inventory.commands",
		"PAYMENT_COMMANDS_TOPIC":      "payment.commands",
		"NOTIFICATION_COMMANDS_TOPIC": "notification.commands",
		"INVENTORY_REPLIES_TOPIC":     "inventory.replies",
		"PAYMENT_REPLIES_TOPIC":       "payment.replies",
		"NOTIFICATION_REPLIES_TOPIC":  "notification.replies",
		"ORDERS_DLQ_TOPIC":            "orders.dlq",
		"SAGA_ORCHESTRATOR_GROUP":     "saga-orchestrator",
		"INVENTORY_SERVICE_GROUP":     "inventory-service",
		"PAYMENT_SERVICE_GROUP":       "payment-service",
		"NOTIFICATION_SERVICE_GROUP":  "notification-service",
		"REGION":                      "US",
	}

	for key, value := range envVars {
		os.Setenv(key, value)
	}
	defer func() {
		for key := range envVars {
			os.Unsetenv(key)
		}
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Validate Kafka config
	if len(cfg.Kafka.Brokers) != 1 || cfg.Kafka.Brokers[0] != "localhost:9092" {
		t.Errorf("Expected Kafka brokers [localhost:9092], got %v", cfg.Kafka.Brokers)
	}
	if cfg.Kafka.MaxRequestRetries != 5 {
		t.Errorf("Expected Kafka max request retries 5, got %d", cfg.Kafka.MaxRequestRetries)
	}
	if cfg.Kafka.MaxRecordRetries != 10 {
		t.Errorf("Expected Kafka max record retries 10, got %d", cfg.Kafka.MaxRecordRetries)
	}

	// Validate Postgres config
	if cfg.Postgres.User != "test_user" {
		t.Errorf("Expected Postgres user 'test_user', got '%s'", cfg.Postgres.User)
	}
	if cfg.Postgres.Password != "test_pass" {
		t.Errorf("Expected Postgres password 'test_pass', got '%s'", cfg.Postgres.Password)
	}
	if cfg.Postgres.DB != "test_db" {
		t.Errorf("Expected Postgres DB 'test_db', got '%s'", cfg.Postgres.DB)
	}
	if cfg.Postgres.Host != "localhost" {
		t.Errorf("Expected Postgres host 'localhost', got '%s'", cfg.Postgres.Host)
	}
	if cfg.Postgres.Port != 5432 {
		t.Errorf("Expected Postgres port 5432, got %d", cfg.Postgres.Port)
	}

	// Validate Redis config
	if cfg.Redis.Host != "localhost" {
		t.Errorf("Expected Redis host 'localhost', got '%s'", cfg.Redis.Host)
	}
	if cfg.Redis.Port != 6379 {
		t.Errorf("Expected Redis port 6379, got %d", cfg.Redis.Port)
	}

	// Validate region
	if cfg.Region != "US" {
		t.Errorf("Expected region 'US', got '%s'", cfg.Region)
	}
}

func TestParseBrokers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single broker",
			input:    "localhost:9092",
			expected: []string{"localhost:9092"},
		},
		{
			name:     "multiple brokers",
			input:    "localhost:9092,localhost:9093,localhost:9094",
			expected: []string{"localhost:9092", "localhost:9093", "localhost:9094"},
		},
		{
			name:     "brokers with spaces",
			input:    "localhost:9092, localhost:9093, localhost:9094",
			expected: []string{"localhost:9092", "localhost:9093", "localhost:9094"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBrokers(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d brokers, got %d", len(tt.expected), len(result))
			}
			for i, broker := range result {
				if broker != tt.expected[i] {
					t.Errorf("Expected broker '%s', got '%s'", tt.expected[i], broker)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				Kafka: KafkaConfig{
					Brokers: []string{"localhost:9092"},
				},
				Postgres: PostgresConfig{
					User:     "user",
					Password: "pass",
					DB:       "db",
					Host:     "localhost",
					Port:     5432,
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				Topics: TopicsConfig{
					Commands: CommandTopics{
						Orders:       "orders",
						Inventory:    "inventory.commands",
						Payment:      "payment.commands",
						Notification: "notification.commands",
					},
					Replies: ReplyTopics{
						Inventory:    "inventory.replies",
						Payment:      "payment.replies",
						Notification: "notification.replies",
					},
					DLQ: DLQTopics{
						Orders: "orders.dlq",
					},
				},
				ConsumerGroups: ConsumerGroupsConfig{
					SagaOrchestrator:    "saga-orchestrator",
					InventoryService:    "inventory-service",
					PaymentService:      "payment-service",
					NotificationService: "notification-service",
				},
				Region: "US",
			},
			expectError: false,
		},
		{
			name: "missing kafka brokers",
			config: &Config{
				Kafka: KafkaConfig{
					Brokers: []string{},
				},
			},
			expectError: true,
			errorMsg:    "KAFKA_BROKERS is required",
		},
		{
			name: "missing postgres user",
			config: &Config{
				Kafka: KafkaConfig{
					Brokers: []string{"localhost:9092"},
				},
				Postgres: PostgresConfig{
					User: "",
				},
			},
			expectError: true,
			errorMsg:    "POSTGRES_USER is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("Expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestGetPostgresConnectionString(t *testing.T) {
	cfg := &Config{
		Postgres: PostgresConfig{
			User:     "test_user",
			Password: "test_pass",
			DB:       "test_db",
			Host:     "localhost",
			Port:     5432,
		},
	}

	expected := "host=localhost port=5432 user=test_user password=test_pass dbname=test_db sslmode=disable"
	result := cfg.GetPostgresConnectionString()

	if result != expected {
		t.Errorf("Expected connection string '%s', got '%s'", expected, result)
	}
}

func TestGetRedisAddress(t *testing.T) {
	cfg := &Config{
		Redis: RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
	}

	expected := "localhost:6379"
	result := cfg.GetRedisAddress()

	if result != expected {
		t.Errorf("Expected Redis address '%s', got '%s'", expected, result)
	}
}
