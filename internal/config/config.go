package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	Kafka          KafkaConfig
	Postgres       PostgresConfig
	Redis          RedisConfig
	Topics         TopicsConfig
	ConsumerGroups ConsumerGroupsConfig
	Region         string
}

// KafkaConfig holds Kafka-related configuration
type KafkaConfig struct {
	Brokers           []string
	MaxRequestRetries int
	MaxRecordRetries  int
}

// PostgresConfig holds PostgreSQL-related configuration
type PostgresConfig struct {
	User     string
	Password string
	DB       string
	Host     string
	Port     int
}

// RedisConfig holds Redis-related configuration
type RedisConfig struct {
	Host string
	Port int
}

// TopicsConfig holds all Kafka topic names
type TopicsConfig struct {
	Commands CommandTopics
	Replies  ReplyTopics
	DLQ      DLQTopics
}

// CommandTopics holds command topic names
type CommandTopics struct {
	Orders       string
	Inventory    string
	Payment      string
	Notification string
}

// ReplyTopics holds reply topic names
type ReplyTopics struct {
	Inventory    string
	Payment      string
	Notification string
}

// DLQTopics holds dead letter queue topic names
type DLQTopics struct {
	Orders string
}

// ConsumerGroupsConfig holds all consumer group IDs
type ConsumerGroupsConfig struct {
	SagaOrchestrator    string
	InventoryService    string
	PaymentService      string
	NotificationService string
}

// Load reads configuration from environment variables using Viper
func Load() (*Config, error) {
	v := viper.New()

	// Set the file name of the configurations file
	v.SetConfigFile(".env")

	// Enable automatic env variable reading
	v.AutomaticEnv()

	// Try to read the config file, but don't fail if it doesn't exist
	// Environment variables will still be read
	if err := v.ReadInConfig(); err != nil {
		// Check if it's a file not found error
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// If file not found, we'll just use environment variables
	}

	// Build the config struct
	cfg := &Config{
		Kafka: KafkaConfig{
			Brokers:           parseBrokers(v.GetString("KAFKA_BROKERS")),
			MaxRequestRetries: v.GetInt("MAX_REQ_RETRIES"),
			MaxRecordRetries:  v.GetInt("MAX_RECORD_RETRIES"),
		},
		Postgres: PostgresConfig{
			User:     v.GetString("POSTGRES_USER"),
			Password: v.GetString("POSTGRES_PASSWORD"),
			DB:       v.GetString("POSTGRES_DB"),
			Host:     v.GetString("POSTGRES_HOST"),
			Port:     v.GetInt("POSTGRES_PORT"),
		},
		Redis: RedisConfig{
			Host: v.GetString("REDIS_HOST"),
			Port: v.GetInt("REDIS_PORT"),
		},
		Topics: TopicsConfig{
			Commands: CommandTopics{
				Orders:       v.GetString("ORDERS_TOPIC"),
				Inventory:    v.GetString("INVENTORY_COMMANDS_TOPIC"),
				Payment:      v.GetString("PAYMENT_COMMANDS_TOPIC"),
				Notification: v.GetString("NOTIFICATION_COMMANDS_TOPIC"),
			},
			Replies: ReplyTopics{
				Inventory:    v.GetString("INVENTORY_REPLIES_TOPIC"),
				Payment:      v.GetString("PAYMENT_REPLIES_TOPIC"),
				Notification: v.GetString("NOTIFICATION_REPLIES_TOPIC"),
			},
			DLQ: DLQTopics{
				Orders: v.GetString("ORDERS_DLQ_TOPIC"),
			},
		},
		ConsumerGroups: ConsumerGroupsConfig{
			SagaOrchestrator:    v.GetString("SAGA_ORCHESTRATOR_GROUP"),
			InventoryService:    v.GetString("INVENTORY_SERVICE_GROUP"),
			PaymentService:      v.GetString("PAYMENT_SERVICE_GROUP"),
			NotificationService: v.GetString("NOTIFICATION_SERVICE_GROUP"),
		},
		Region: v.GetString("REGION"),
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate checks that all required configuration values are present and valid
func (c *Config) Validate() error {
	// Validate Kafka config
	if len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS is required")
	}

	if c.Kafka.MaxRecordRetries == 0 {
		c.Kafka.MaxRecordRetries = 10
	}

	if c.Kafka.MaxRequestRetries == 0 {
		c.Kafka.MaxRequestRetries = 10
	}

	// Validate Postgres config
	if c.Postgres.User == "" {
		return fmt.Errorf("POSTGRES_USER is required")
	}
	if c.Postgres.Password == "" {
		return fmt.Errorf("POSTGRES_PASSWORD is required")
	}
	if c.Postgres.DB == "" {
		return fmt.Errorf("POSTGRES_DB is required")
	}
	if c.Postgres.Host == "" {
		return fmt.Errorf("POSTGRES_HOST is required")
	}
	if c.Postgres.Port == 0 {
		return fmt.Errorf("POSTGRES_PORT is required")
	}

	// Validate Redis config
	if c.Redis.Host == "" {
		return fmt.Errorf("REDIS_HOST is required")
	}
	if c.Redis.Port == 0 {
		return fmt.Errorf("REDIS_PORT is required")
	}

	// Validate command topics
	if c.Topics.Commands.Orders == "" {
		return fmt.Errorf("ORDERS_TOPIC is required")
	}
	if c.Topics.Commands.Inventory == "" {
		return fmt.Errorf("INVENTORY_COMMANDS_TOPIC is required")
	}
	if c.Topics.Commands.Payment == "" {
		return fmt.Errorf("PAYMENT_COMMANDS_TOPIC is required")
	}
	if c.Topics.Commands.Notification == "" {
		return fmt.Errorf("NOTIFICATION_COMMANDS_TOPIC is required")
	}

	// Validate reply topics
	if c.Topics.Replies.Inventory == "" {
		return fmt.Errorf("INVENTORY_REPLIES_TOPIC is required")
	}
	if c.Topics.Replies.Payment == "" {
		return fmt.Errorf("PAYMENT_REPLIES_TOPIC is required")
	}
	if c.Topics.Replies.Notification == "" {
		return fmt.Errorf("NOTIFICATION_REPLIES_TOPIC is required")
	}

	// Validate DLQ topics
	if c.Topics.DLQ.Orders == "" {
		return fmt.Errorf("ORDERS_DLQ_TOPIC is required")
	}

	// Validate consumer groups
	if c.ConsumerGroups.SagaOrchestrator == "" {
		return fmt.Errorf("SAGA_ORCHESTRATOR_GROUP is required")
	}
	if c.ConsumerGroups.InventoryService == "" {
		return fmt.Errorf("INVENTORY_SERVICE_GROUP is required")
	}
	if c.ConsumerGroups.PaymentService == "" {
		return fmt.Errorf("PAYMENT_SERVICE_GROUP is required")
	}
	if c.ConsumerGroups.NotificationService == "" {
		return fmt.Errorf("NOTIFICATION_SERVICE_GROUP is required")
	}

	// Validate region
	if c.Region == "" {
		return fmt.Errorf("REGION is required")
	}

	return nil
}

// parseBrokers splits comma-separated broker addresses
func parseBrokers(brokers string) []string {
	if brokers == "" {
		return []string{}
	}

	parts := strings.Split(brokers, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// GetPostgresConnectionString returns a formatted PostgreSQL connection string
func (c *Config) GetPostgresConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		c.Postgres.Host,
		c.Postgres.Port,
		c.Postgres.User,
		c.Postgres.Password,
		c.Postgres.DB,
	)
}

// GetRedisAddress returns the Redis address in host:port format
func (c *Config) GetRedisAddress() string {
	return fmt.Sprintf("%s:%d", c.Redis.Host, c.Redis.Port)
}
