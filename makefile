.PHONY: help setup start stop restart clean logs db-seed test-reserve test-release run-inventory run-orchestrator test-integration test-saga dev db-check watch-commands watch-replies

# Default target
help:
	@echo "Available commands:"
	@echo "  make setup            - Initial project setup (install deps, create .env)"
	@echo "  make start            - Start all infrastructure (docker-compose up)"
	@echo "  make stop             - Stop all infrastructure"
	@echo "  make restart          - Restart infrastructure"
	@echo "  make clean            - Stop and remove all containers, volumes"
	@echo "  make logs             - Show docker logs"
	@echo "  make run-inventory    - Run inventory service locally"
	@echo "  make run-orchestrator - Run saga orchestrator locally"
	@echo "  make run-payment      - Run payment service locally"
	@echo "  make test-saga        - Run live saga end-to-end test (requires running services)"
	@echo "  make test-integration - Run integration tests (requires Docker)"
	@echo "  make dev              - Full dev setup (start + seed + run services)"

# Install Go dependencies
setup:
	@echo "Installing Go dependencies..."
	go mod download
	go mod tidy
	@echo "Creating .env file if it doesn't exist..."
	@test -f .env || cp .env.example .env
	@echo "Setup complete! 🥳"

# Start infrastructure
start:
	@echo "Starting infrastructure..."
	docker-compose up -d
	@echo "Waiting for services to be ready..."
	@sleep 10
	@echo "Infrastructure started! Kafka UI: http://localhost:8080"

# Stop infrastructure
stop:
	@echo "Stopping infrastructure..."
	docker-compose stop

# Restart infrastructure
restart: stop start

# Clean everything
clean:
	@echo "Cleaning up..."
	docker-compose down -v
	@echo "Cleanup complete!"

# Show logs
logs:
	docker-compose logs -f

# Run live saga test (requires: run-inventory + run-payment + run-orchestrator in separate terminals)
test-saga:
	@echo "Running saga end-to-end test..."
	go run scripts/test-saga/main.go

# Run integration tests
test-integration:
	go test -v -tags=integration -count=1 -timeout=120s ./cmd/inventory/
	go test -v -tags=integration -count=1 -timeout=120s ./cmd/payment/
	go test -v -tags=integration -count=1 -timeout=180s ./cmd/orchestrator/

# Run inventory service
run-inventory:
	@echo "Starting Inventory Service..."
	go run cmd/inventory/main.go

# Run payment service
run-payment:
	@echo "Starting Payment Service..."
	go run cmd/payment/main.go

# Run saga orchestrator
run-orchestrator:
	@echo "Starting Saga Orchestrator..."
	go run cmd/orchestrator/main.go

# Check database
db-check:
	@docker exec -it postgres psql -U kafka_user -d orders_db -c "\
		SELECT product_id, name, stock FROM product; \
		SELECT * FROM inventory_reservations ORDER BY created_at DESC LIMIT 10;"

# Watch Kafka topics
watch-commands:
	@docker exec -it kafka kafka-console-consumer \
		--bootstrap-server localhost:9092 \
		--topic inventory.commands \
		--from-beginning

watch-replies:
	@docker exec -it kafka kafka-console-consumer \
		--bootstrap-server localhost:9092 \
		--topic inventory.replies \
		--from-beginning
