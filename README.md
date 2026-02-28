[![Tests](https://github.com/mateusmlo/altimit-ecomm/actions/workflows/test.yml/badge.svg)](https://github.com/mateusmlo/altimit-ecomm/actions/workflows/test.yml)

# altimit-ecomm

A backend for an e-commerce platform built around the **Saga choreography/orchestration pattern**. Each domain concern (inventory, payment, notification, …) lives in its own service and communicates exclusively through Kafka events — no direct service-to-service calls.

The project is a work in progress. Only the **inventory service** is fully implemented so far; the remaining services (payment, notification, orchestrator) are scaffolded and will follow the same structure.

---

## Stack

| Concern | Technology |
|---|---|
| Language | Go 1.24 |
| Messaging | Apache Kafka (franz-go client) |
| Database | PostgreSQL 17 + GORM |
| Cache | Redis 7 |
| JSON | bytedance/sonic |
| Config | Viper (env vars / `.env` file) |
| Testing | testify + Testcontainers |
| Infrastructure | Docker Compose |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Kafka topics                            │
│                                                                 │
│  orders           inventory.commands       payment.commands     │
│  inventory.replies  payment.replies        notification.*       │
│  orders.dlq                                                     │
└───────────────────────────┬─────────────────────────────────────┘
                            │ consume / produce
           ┌────────────────┼────────────────┐
           ▼                ▼                ▼
   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
   │  Inventory   │  │   Payment    │  │ Notification │
   │   Service    │  │   Service    │  │   Service    │
   └──────┬───────┘  └──────────────┘  └──────────────┘
          │ GORM
          ▼
     PostgreSQL
```

### Key design decisions

**Saga pattern over direct calls** — each service only knows about Kafka topics. Commands arrive as records on `*.commands` topics; replies are published back to `*.replies` topics. This keeps services fully decoupled and makes compensating transactions explicit.

**Idempotency keys** — every command is guarded by an idempotency key stored in PostgreSQL so that retries and at-least-once delivery do not produce duplicate side effects.

**Sentinel errors in `internal/errs`** — all shared domain errors (`ErrProductNotFound`, `ErrInsufficientStock`, `ErrMissingMetadata`, …) live in one package so any internal package can check them with `errors.Is` without creating circular imports.

**Record metadata in Kafka headers** — event type, saga ID, order ID, and timestamp are serialised into a `metadata` header on every record, keeping the message body as a plain command/reply payload.

**GORM auto-migrate** — schema migrations run at startup via `AutoMigrate`. Suitable for development; a proper migration tool (Atlas, golang-migrate) would replace this in production.

---

## Project layout

```
cmd/
  inventory/        # Inventory service entrypoint + integration tests
  payment/          # (scaffolded)
  notification/     # (scaffolded)
  orchestrator/     # (scaffolded)
internal/
  config/           # Viper-based config loader (see internal/config/README.md)
  database/         # GORM connection setup
  errs/             # Shared sentinel errors
  inventory/        # Inventory service logic and Kafka handler
  kafka/            # Generic Kafka consumer and producer
  models/           # GORM models and event/command types
  repository/       # Data-access layer (one file per aggregate)
```

---

## Running locally

### Prerequisites

- Go 1.24+
- Docker + Docker Compose

### 1. First-time setup

```bash
make setup        # downloads Go deps, copies .env.example → .env
```

Edit `.env` if you need to change any defaults. See [`internal/config/README.md`](internal/config/README.md) for the full list of variables.

### 2. Start infrastructure

```bash
make start        # postgres, kafka, redis, kafka-ui
```

Kafka UI is available at <http://localhost:8080>.

### 3. Seed the database

```bash
make db-seed      # inserts test products into postgres
```

### 4. Run the inventory service

```bash
make run-inventory
```

### 5. Send a test command

```bash
make test-reserve   # publishes a ReserveInventory command to Kafka
make test-release   # publishes a ReleaseInventory command to Kafka
```

### Shortcuts

```bash
make dev          # start + seed in one step
make logs         # tail docker-compose logs
make clean        # tear down containers and volumes
```

---

## Testing

### Unit tests

```bash
go test ./...
```

No external services required — repository tests are excluded unless the `integration` build tag is set.

### Integration tests

Integration tests use **Testcontainers** to spin up real PostgreSQL and Kafka instances automatically.

```bash
make test-integration
# or directly:
go test -v -tags=integration -count=1 -timeout=120s ./...
```

Docker must be running. The first run will pull the container images.

---

## CI

GitHub Actions runs both test suites on every push and pull request to `main`. See [`.github/workflows/test.yml`](.github/workflows/test.yml).
