[![Tests](https://github.com/mateusmlo/altimit-ecomm/actions/workflows/test.yml/badge.svg)](https://github.com/mateusmlo/altimit-ecomm/actions/workflows/test.yml)

# altimit-ecomm

A backend for an e-commerce platform built around the **Saga orchestration pattern**. Each domain concern (inventory, payment, notification) lives in its own service and communicates exclusively through Kafka events — no direct service-to-service calls. A central **saga orchestrator** coordinates the distributed transaction and drives compensation when a step fails.

## Disclaimer
This project serves as a means for me to study both Apache Kafka, golang and SAGA pattern implementation, and it's developed using Claude Code purely as a mentor, meaning I use it mainly to explain complex logic, code reviewing and also helping me tackle the project step by step (it's a BIG one). While Claude Code is EXTREMELY good at doing things, I do not rely solely on its ideas, which at times may be questionable or subpar; this is where my experience ~~(and Reddit)~~ comes into play. What I usually ask Claude Code to write: tests, READMEs, scripts, commits, and boilerplate. You know, the boring stuff (which I thorougly review). It's been very challenging and a lot of fun building this project.

---

## Stack

| Concern | Technology |
|---|---|
| Language | Go 1.24 |
| Messaging | Apache Kafka (franz-go client) |
| Database | PostgreSQL 17 + GORM |
| Payments | Stripe (stripe-go v85) |
| JSON | bytedance/sonic |
| Config | Viper (env vars / `.env` file) |
| Testing | testify + Testcontainers |
| Infrastructure | Docker Compose |

---

## Architecture

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │                            Kafka topics                              │
  │                                                                      │
  │  orders          inventory.commands       payment.commands           │
  │  inventory.replies  payment.replies       notification.commands      │
  │  notification.replies                     orders.dlq                 │
  └──────┬────────────────────┬───────────────────────┬─────────────────┘
         │ consume/produce     │ consume/produce        │ consume/produce
         ▼                     ▼                        ▼
  ┌─────────────┐      ┌──────────────┐       ┌──────────────────┐
  │  Inventory  │      │   Payment    │       │  Notification    │
  │   Service   │      │   Service    │       │    Service       │
  └──────┬──────┘      └──────┬───────┘       └──────────────────┘
         │ GORM                │ GORM + Stripe
         ▼                     ▼
    PostgreSQL            PostgreSQL
         ▲
         │ GORM
  ┌──────┴──────────────────────────────┐
  │         Saga Orchestrator           │
  │  • drives the order workflow        │
  │  • triggers compensation on failure │
  │  • retries failed compensations     │
  │  • alerts DLQ on permanent failure  │
  └─────────────────────────────────────┘
```

### Order workflow

```
START
  │
  ▼
[ReserveInventory] ──fail──► (no prior steps — mark FAILED)
  │ success
  ▼
[ProcessPayment] ──fail──► [ReleaseInventory] ──► COMPENSATED / COMPENSATION_FAILED
  │ success
  ▼
[SendNotification] ──fail──► [RefundPayment] ──► [ReleaseInventory] ──► COMPENSATED
  │ success
  ▼
COMPLETED
```

### Saga states

| Status | Meaning |
|---|---|
| `STARTED` | Saga created, first command published |
| `IN_PROGRESS` | A forward step is executing |
| `COMPLETED` | All steps succeeded |
| `COMPENSATING` | A step failed; compensation is running |
| `COMPENSATED` | Compensation succeeded; order rolled back |
| `FAILED` | First step failed; nothing to compensate |
| `COMPENSATION_FAILED` | Max compensation retries exhausted; requires manual intervention |
| `CANCELLED` | Saga cancelled before completion |

### Key design decisions

**Saga orchestration** — the orchestrator holds the full workflow definition. It publishes commands to `*.commands` topics and listens for replies on `*.replies` topics. On failure it walks backwards through completed steps and triggers compensations in order.

**Compensation routing via workflow metadata** — each `StepDefinition` carries its own `CompensationStep`, `CompensationEventType`, and `CompensationCommandTopic` so compensation commands are always routed to the correct service topic without any switch logic outside the workflow definition.

**Exponential backoff for failed compensations** — if a compensation step itself fails, the orchestrator records the retry count and a `NextRetryAt` timestamp (initial backoff 1s, multiplier 2×, cap 60s, ±20% jitter). A `RetryWorker` goroutine polls every 5 seconds for sagas past their retry time and re-publishes the compensation command. After `MaxCompensationRetries` (3) the saga is marked `COMPENSATION_FAILED` and a `CompensationFailedPayload` alert is published to the DLQ for manual intervention.

**Dead-letter queue for permanent failures** — handler errors that survive all retries are forwarded to `orders.dlq` by the Kafka consumer with a `dlq-error` header containing the original error. This keeps the consumer unblocked without silently losing records.

**Idempotency via EventID** — every command handler checks the incoming `EventID` (a UUID generated by the orchestrator and carried in a Kafka record header) against an `idempotency_keys` table before dispatching to the service. On a hit the handler logs and returns without re-running the side effect. On a miss it dispatches, publishes the reply, then stores the key with the reply payload. Storing *after* a successful publish means a publish failure leaves no key behind, so the command is safely retried. This guards the payment (Stripe charges) and inventory (reservations) handlers against Kafka's at-least-once redelivery. The orchestrator handler is guarded by its own saga state machine instead.

**Record metadata in Kafka headers** — `EventType`, `EventID`, `SagaID`, `OrderID`, and `Timestamp` are serialised into a `metadata` header on every record. This keeps the message body as a plain command/reply payload and lets any consumer extract routing information without unmarshaling the full body.

**`EventPublisher` interface for testability** — the Kafka producer is depended on through a single-method interface (`kafka.EventPublisher`). Handlers and the orchestrator take the interface, not the concrete `*kafka.Producer`. This lets unit tests inject a simple `mockPublisher` with no broker required.

**Stripe errors are not infrastructure errors** — the payment service treats card declines, failed refunds, and Stripe API responses as business outcomes, not crashes. These produce `PAYMENT_FAILED` / `REFUND_FAILED` replies so the saga can compensate gracefully. Only true infrastructure failures (DB errors, serialisation errors) bubble up and stop the consumer.

**Sentinel errors in `internal/errs`** — all shared domain errors live in one package so any internal package can use `errors.Is` without creating circular imports.

---

## Project layout

```
cmd/
  inventory/        # Inventory service entrypoint + integration tests
  notification/     # Notification service entrypoint (scaffolded)
  orchestrator/     # Saga orchestrator entrypoint + integration tests
  payment/          # Payment service entrypoint + integration tests
internal/
  config/           # Viper-based config loader (see internal/config/README.md)
  database/         # GORM connection setup and auto-migration
  errs/             # Shared sentinel errors
  inventory/        # Inventory service logic and Kafka handler
  kafka/            # Generic consumer, producer, DLQ publisher, EventPublisher interface
  models/           # GORM models, event/command types, saga workflow definition
  orchestrator/     # Saga orchestrator logic and Kafka handler
  payment/          # Payment service logic and Kafka handler
  repository/       # Data-access layer (one file per aggregate)
  stripe/           # Stripe client, StripeService interface, and stripetest fake
scripts/
  migrate.sql       # Idempotent DB migrations for existing containers
  seed.sql          # Test product data
  test-saga/        # Live end-to-end saga test (Go program)
```

### Database tables

| Table | Model | Notes |
|---|---|---|
| `orders` | `Order` | UUID PK, `order_status` enum |
| `order_items` | `OrderItem` | FK → orders |
| `products` | `Product` | UUID PK, stock with CHECK constraints |
| `inventory_reservations` | `InventoryReservation` | `reservation_status` enum (ACTIVE / RELEASED / CONFIRMED) |
| `payments` | `Payment` | `payment_status` enum, Stripe `PaymentIntentID` |
| `saga_states` | `SagaState` | `saga_status` enum, `current_step`, `compensation_retries`, `next_retry_at`, JSONB payload |
| `idempotency_keys` | `IdempotencyKey` | EventID string PK, JSONB response, `created_at` |

All tables are auto-migrated on service startup via GORM.

---

## Running locally

### Prerequisites

- Go 1.24+
- Docker + Docker Compose
- A Stripe account (test-mode secret key)

### 1. First-time setup

```bash
make setup        # downloads Go deps, copies .env.example → .env
```

Edit `.env` and set `STRIPE_SECRET_KEY` to your Stripe test-mode secret key. See [`internal/config/README.md`](internal/config/README.md) for the full list of variables.

### 2. Start infrastructure

```bash
make start        # postgres, kafka, redis, kafka-ui
```

Kafka UI is available at <http://localhost:8080>.

### 3. Seed the database

```bash
make db-seed      # inserts test products into postgres
```

If you are running an existing container that predates the current schema, apply the idempotent migrations first:

```bash
make db-migrate
```

### 4. Run the services

Open three terminals:

```bash
# terminal 1
make run-inventory

# terminal 2
make run-payment

# terminal 3
make run-orchestrator
```

### 5. Run a live saga test

```bash
make test-saga
```

This creates a real order and publishes it to Kafka. The inventory service handles stock reservation, the payment service charges via Stripe (test mode), and the notification service is stubbed by the test script. All three services must be running.

### Shortcuts

```bash
make dev          # start + seed in one step, then prints next steps
make logs         # tail docker-compose logs
make clean        # tear down containers and volumes
```

---

## Testing

### Unit tests

```bash
go test ./...
```

No external services required. Handler tests use hand-written mocks for all dependencies; the `kafka.EventPublisher` interface means no Kafka broker is needed.

### Integration tests

Integration tests use **Testcontainers** to spin up real PostgreSQL and Kafka instances automatically. Docker must be running. The payment service integration tests use a configurable `FakeService` Stripe double — no real Stripe API calls are made.

```bash
make test-integration
```

This runs the inventory, payment, and orchestrator integration suites. The first run will pull the container images.

### Test layout

| Package | Test type | What it covers |
|---|---|---|
| `internal/repository/` | Integration | All repository CRUD operations against a real Postgres container |
| `internal/inventory/` | Unit | Handler idempotency, service reservation/release logic |
| `internal/payment/` | Unit | Handler idempotency, service payment/refund dispatch |
| `internal/orchestrator/` | Unit | Full saga state machine: step advance, compensation chain, retry backoff |
| `internal/kafka/` | Unit | Consumer error handling, DLQ forwarding, producer publish |
| `cmd/payment/` | Integration | End-to-end payment saga flow with real DB and Kafka |
| `cmd/inventory/` | Integration | End-to-end inventory saga flow with real DB and Kafka |
| `cmd/orchestrator/` | Integration | Full saga orchestration, compensation, and DLQ alert |

---

## CI

GitHub Actions runs both test suites on every push and pull request to `main`. See [`.github/workflows/test.yml`](.github/workflows/test.yml).
