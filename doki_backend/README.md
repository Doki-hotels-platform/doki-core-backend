# DOKI Hotels Backend 🏨

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16%20(pgxpool)-336791?style=flat&logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7%20(Lua%20Engine)-DC382D?style=flat&logo=redis)](https://redis.io/)
[![Architecture](https://img.shields.io/badge/Architecture-Hexagonal%20%2F%20Clean-orange?style=flat)](https://alistair.cockburn.us/hexagonal-architecture/)
[![Observability](https://img.shields.io/badge/Observability-Prometheus%20%2B%20slog-E6522C?style=flat&logo=prometheus)](https://prometheus.io/)
[![License](https://img.shields.io/badge/License-Proprietary-red)](#)

> **Enterprise Property Management System (PMS) & Central Reservation System (CRS) MVP**  
> High-concurrency, two-tier inventory locking, hierarchical RBAC, and double-entry financial accounting backend for hotel networks and serviced residences.

---

## 1. Project Overview & Architecture

The **DOKI Hotels Backend** is a high-performance modular monolith built in Go. It serves as the core transaction engine across five client surfaces: public booking web, customer mobile app (iOS/Android), hotel front-desk operations portal (PMS), and HQ administration console.

### Transaction Backbone
- **Central Reservation System (CRS):** High-speed multi-property availability search, date-range pricing calculation, and short-lived inventory hold engine.
- **Property Management System (PMS):** Front-desk stay management, room assignment with exclusion constraints, check-in, check-out, and cash receipt reconciliation.
- **Two-Tier Inventory Engine:** Low-latency atomic hold acquisition via Redis Lua scripts (Layer 1) paired with authoritative PostgreSQL capacity serialization (`SELECT ... FOR UPDATE` & `chk_inventory_bounds`) (Layer 2).
- **Payment & Gateway Orchestration:** Multi-provider payment adapter architecture supporting Telebirr, CBE Birr, and Chapa with fail-closed webhook signature verification.
- **Double-Entry Financial Ledger:** Immutable, append-only accounting subsystem with zero floating-point math, supporting automated commission calculations and hotel payout settlement.

---

### Hexagonal Architecture / Ports & Adapters

```
                     ┌─────────────────────────────────────────────────────────┐
                     │                   DRIVING ADAPTERS                      │
                     │  ┌─────────────────────────┐  ┌──────────────────────┐  │
                     │  │   HTTP Router (Chi/v5)  │  │  Background Workers  │  │
                     │  │  JWT / RBAC Middleware  │  │  - Hold Sweeper      │  │
                     │  │  Idempotency Middleware │  │  - Outbox Dispatcher │  │
                     │  └───────────┬─────────────┘  └──────────┬───────────┘  │
                     └──────────────┼───────────────────────────┼──────────────┘
                                    │ (Calls Domain Ports)      │
                                    ▼                           ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                                       CORE DOMAIN LAYER                                     │
│                                (Pure Go — Zero External Dependencies)                       │
│                                                                                             │
│   ┌──────────────────────┐   ┌──────────────────────┐   ┌───────────────────────────────┐   │
│   │   Identity Domain    │   │   Property Domain    │   │       Inventory Domain        │   │
│   │ - User & Staff Auth  │   │ - Property Profiles  │   │ - Hold Orchestration          │   │
│   │ - Role Guard Rules   │   │ - Room Types & Units │   │ - Allocation Engine           │   │
│   └──────────────────────┘   └──────────────────────┘   └───────────────────────────────┘   │
│   ┌──────────────────────┐   ┌──────────────────────┐   ┌───────────────────────────────┐   │
│   │  Reservation Domain  │   │      PMS Domain      │   │        Billing Domain         │   │
│   │ - Booking State Mach │   │ - Stay Management    │   │ - Double-Entry Ledger         │   │
│   │ - Hold Compensation  │   │ - Physical Assignment│   │ - Commission Rules            │   │
│   └──────────────────────┘   └──────────────────────┘   └───────────────────────────────┘   │
│                                                                                             │
│   PORT INTERFACES (domain/ports.go):                                                        │
│   - domain.InventoryRepository     - domain.ReservationRepository   - domain.PaymentGateway │
└──────────────────────────────────────────────▲──────────────────────────────────────────────┘
                                               │ (Implemented by Driven Adapters)
                     ┌─────────────────────────┴───────────────────────────────┐
                     │                    DRIVEN ADAPTERS                      │
                     │  ┌───────────────────────┐   ┌───────────────────────┐  │
                     │  │  PostgreSQL (pgx/v5)  │   │     Redis (v9)        │  │
                     │  │  - pgxpool Pool       │   │  - Atomic Lua Holds   │  │
                     │  │  - Row-Level Locking  │   │  - Idempotency Store  │  │
                     │  └───────────────────────┘   └───────────────────────┘  │
                     │  ┌───────────────────────┐   ┌───────────────────────┐  │
                     │  │   Payment Gateways    │   │  Identity / Telecom   │  │
                     │  │  - Telebirr / CBE /   │   │  - Fayda Auth Adapter │  │
                     │  │    Chapa Adapters     │   │  - SMS/Email Gateways │  │
                     │  └───────────────────────┘   └───────────────────────┘  │
                     └─────────────────────────────────────────────────────────┘
```

---

### Core Design Principles

1. **Authoritative PostgreSQL State vs. Fast-Path Redis Lua Holds:**
   - Redis acts as a high-throughput, low-latency reservation buffer (UX-optimizing, short TTL).
   - PostgreSQL is the single source of truth. Even if Redis experiences eviction, node restarts, or state desynchronization, the database enforces hard table check constraints (`chk_inventory_bounds`), rendering overselling mathematically impossible.
2. **Zero-Dependency Domain Boundary:**
   - `internal/domain` contains zero imports from `net/http`, `github.com/jackc/pgx`, `github.com/redis/go-redis`, or `github.com/go-chi/chi`.
   - All external interactions occur via Go interfaces (Ports). Enforcement is automated in CI via `golangci-lint` (`depguard`).
3. **Reversible, Append-Only Financial Ledger:**
   - Monetary values are represented in minor units (`AmountMinor int64`, `Currency string`) to eliminate IEEE-754 floating-point errors.
   - Financial transactions follow double-entry rules (Debits equal Credits). Database rows in the ledger are immutable—corrections occur exclusively via compensating reversal entries.

---

## 2. Repository Layout & Directory Structure

```
doki-backend/
├── cmd/
│   ├── api/                      # Main HTTP REST API entrypoint and dependency injection wiring
│   │   └── main.go
│   ├── worker/                   # Background tasks: outbox message relay & expired hold sweeper
│   │   └── main.go
│   └── migrate/                  # Database migration runner entrypoint
│       └── main.go
├── internal/
│   ├── domain/                   # Pure business logic and domain abstractions
│   │   ├── errors.go             # Domain sentinel errors (ErrInventoryUnavailable, ErrHoldExpired, etc.)
│   │   ├── models.go             # Core domain models (Reservation, InventoryHold)
│   │   ├── ports.go              # Port interfaces (InventoryRepository, ReservationRepository, PaymentGateway)
│   │   ├── identity/             # Users, roles (HQ/Regional/Owner/Manager/Receptionist/Customer), RBAC guards
│   │   ├── property/             # Properties, room types, physical rooms, rate configurations
│   │   ├── inventory/            # Hold service, daily allocations logic, rollback compensation
│   │   ├── reservation/          # Booking aggregate, lifecycle state machine
│   │   ├── pms/                  # Stays, check-in, check-out, physical room assignment
│   │   ├── billing/              # Double-entry ledger entries, commission engine rules
│   │   └── notification/         # Outbox events and notification definitions
│   ├── adapter/
│   │   ├── repository/postgres/  # pgxpool database adapter implementations of domain ports
│   │   ├── cache/redis/          # Redis Lua scripts, token management, idempotency lock adapter
│   │   ├── integration/
│   │   │   ├── payment/          # Payment adapters: Telebirr, CBE Birr, Chapa
│   │   │   ├── identity/         # Fayda identity verification adapter + manual fallback
│   │   │   └── notification/     # SMS, Email, and Push notification client adapters
│   │   └── http/
│   │       ├── middleware/       # JWT auth, hierarchical RBAC guard, Idempotency-Key, rate limiter
│   │       ├── v1/               # Chi v1 HTTP handlers & DTOs partitioned by domain
│   │       └── webhook/          # Async payment provider webhook callbacks
│   ├── platform/
│   │   ├── database/             # Production PostgreSQL pgxpool setup, tuning & transaction manager
│   │   ├── cache/                # Production Redis connection pool & healthcheck
│   │   ├── logger/               # slog structured JSON logger with request-scoped context tracing
│   │   ├── telemetry/            # Prometheus metrics registry and OpenTelemetry tracing
│   │   └── outbox/               # Transactional outbox polling and publishing engine
│   └── types/                    # Framework-free value objects: Money, Currency, Pagination
├── pkg/
│   └── types/                    # Publicly exportable primitives (Money)
├── migrations/                   # SQL DDL migrations partitioned by bounded context
│   ├── 000001_identity.up.sql
│   ├── 000002_property.up.sql
│   └── 000003_inventory.up.sql
├── deploy/
│   ├── docker/                   # Multi-stage distroless Dockerfile
│   │   └── Dockerfile
│   └── compose/                  # Local developer service compositions
│       └── docker-compose.yml
├── .env.example                  # Environment configuration template
├── .golangci.yml                 # Linting configuration & import boundary enforcement
├── .depguard.yml                 # Depguard rule definitions
├── docker-compose.yml            # Root developer docker stack
├── Dockerfile                    # Multi-stage distroless build recipe
├── Makefile                      # Standard build, test, and container automation
└── go.mod                        # Go module dependencies
```

---

## 3. Local Development & Setup

### Prerequisites
- **Go:** `1.24+` (or `1.22+`)
- **Docker & Docker Compose:** Docker Engine `24+` / Compose `v2+`
- **Make:** GNU Make

### Step-by-Step Installation

#### 1. Clone & Configure Environment
```bash
git clone https://github.com/doki-hotels/doki-backend.git
cd doki-backend

# Copy example environment configuration
cp .env.example .env
```

#### 2. Start PostgreSQL & Redis Services
```bash
make docker-up
```
*This launches PostgreSQL 16 on port `5432` and Redis 7 on port `6379` with health checks.*

#### 3. Verify Database Migrations
Database tables in `./migrations` are automatically initialized on startup via `docker-compose.yml`. To execute migrations manually:
```bash
make migrate-up
```

#### 4. Run API Server
```bash
make run-api
```
*The API server binds to `http://localhost:8080` and the Prometheus metrics server binds to `http://localhost:9090/metrics`.*

---

### Configuration Reference (`.env`)

```ini
# Server Configuration
PORT=8080
METRICS_PORT=9090
ENVIRONMENT=development
LOG_LEVEL=debug

# PostgreSQL Database Configuration (pgxpool)
DATABASE_URL=postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable

# Redis Cache Configuration
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=

# Security & JWT
JWT_SECRET=super-secret-dev-jwt-key-change-in-production
JWT_EXPIRATION_HOURS=24
```

---

## 4. Core Technical Engines & Workflows

### Two-Tier Inventory Concurrency & Locking Engine

To guarantee zero overselling under high concurrency without saturating PostgreSQL with lock contention on every search and hold attempt, DOKI implements a two-tier locking engine:

```
Step 1: Customer selects room & requests hold
                  │
                  ▼
         [Layer 1: Redis Fast-Path]
    Evals inventory_hold.lua atomically:
    1. Checks capacity in Redis
    2. INCR hold count for (property, room_type, date)
    3. Sets 10-minute TTL (inv:hold:...)
    4. Sets lookup token (inv:token:...)
                  │
        ┌─────────┴─────────┐
        ▼                   ▼
  [Redis Sold Out]    [Redis Succeeded]
        │                   │
  Return 409 Conflict       ▼
                     [HoldService Domain]
                     Inserts Reservation (Status: INVENTORY_HOLD)
                            │
                  ┌─────────┴─────────┐
                  ▼                   ▼
             [DB Error]          [DB Success]
                  │                   │
       [Compensating Action]     Return 201 Created
       Calls inventory_release.lua  (Token + Expiration)
       to decrement Redis hold
       immediately (no orphan leak)
```

```
Step 2: Customer completes payment & confirms booking
                  │
                  ▼
       [Layer 2: PostgreSQL Authoritative Validation]
    BEGIN;
    -- Rows locked in ascending stay_date order to prevent deadlocks:
    SELECT id, total_units, allocated_count, blocked_count
    FROM inventory.daily_allocations
    WHERE room_type_id = $1 AND stay_date = $2
    FOR UPDATE;

    -- Validates: allocated_count + blocked_count < total_units
    UPDATE inventory.daily_allocations
    SET allocated_count = allocated_count + 1
    WHERE room_type_id = $1 AND stay_date = $2;

    -- Hard DB constraint chk_inventory_bounds rejects transaction
    -- if allocated_count + blocked_count > total_units
    COMMIT;
```

---

### Reservation Lifecycle State Machine

```
              ┌──────────────────────────┐
              │      INVENTORY_HOLD      │
              └─────────────┬────────────┘
                            │ (Customer selects payment)
                            ▼
              ┌──────────────────────────┐
              │      PAYMENT_PENDING     │
              └──────┬─────────────┬─────┘
   (Payment Fails /  │             │ (Payment Success Webhook)
    Hold Expired)    │             ▼
                     │       ┌───────────┐
                     │       │ CONFIRMED │
                     │       └─────┬─────┘
                     ▼             │ (Day of Stay)
              ┌───────────┐        ▼
              │ CANCELLED │  ┌───────────┐
              └───────────┘  │ EXPECTED  │
                             └─────┬─────┘
                                   │ (Front-Desk Check-In + Fayda ID)
                                   ▼
                             ┌───────────┐
                             │  CHECKED  │
                             │    IN     │
                             └─────┬─────┘
                                   │ (Stay Complete)
                                   ▼
                             ┌───────────┐
                             │  CHECKED  │
                             │    OUT    │
                             └─────┬─────┘
                                   │ (Reconciliation & Payout)
                                   ▼
                             ┌───────────┐
                             │ SETTLED / │
                             │ COMPLETED │
                             └───────────┘
```

---

### Hierarchical Role-Based Access Control (RBAC)

The security middleware enforces hierarchical role clearance and tenant/property scoping:

| Role | Scope | Key Capabilities |
| :--- | :--- | :--- |
| `HQ_ADMIN` | Global | Platform configuration, property onboarding, commissions, global overrides |
| `REGIONAL_SUPERVISOR` | Regional | Regional property visibility, audit review, regional performance monitoring |
| `HOTEL_OWNER` | Multi-Property | Financial ledger reports, payout tracking, staff management |
| `HOTEL_MANAGER` | Property | Room pricing, inventory blocking, rate adjustments, staff operations |
| `RECEPTIONIST` | Property | Guest check-in/out, walk-in reservations, cash receipt recording |
| `CORPORATE` | Account | Corporate rate bookings, invoicing, team reservation management |
| `CUSTOMER` | Self | Public booking search, hold creation, reservation management |

---

## 5. API Endpoints & Contracts (Phase 1)

### Health & Observability Probes

#### `GET /livez`
Liveness probe for orchestrators (Kubernetes / Docker).
- **Status:** `200 OK`
- **Response:**
  ```json
  {
    "status": "UP",
    "version": "1.0.0",
    "timestamp": "2026-08-20T13:30:00Z"
  }
  ```

#### `GET /readyz`
Readiness gate validating live PostgreSQL connection pool and Redis ping before receiving traffic.
- **Status:** `200 OK` (or `503 Service Unavailable`)
- **Response:**
  ```json
  {
    "status": "READY",
    "version": "1.0.0",
    "timestamp": "2026-08-20T13:30:00Z",
    "checks": {
      "postgres": "UP",
      "redis": "UP"
    }
  }
  ```

#### `GET :9090/metrics`
Dedicated Prometheus metrics endpoint exposing HTTP duration histograms, inventory hold counters, conflict rates, and connection pool metrics.

---

### Core Phase-1 API Contracts

#### `GET /v1/properties/search`
Search available properties with aggregated room type availability and rate calculations.

**Query Parameters:**
| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `check_in` | `date` (YYYY-MM-DD) | Yes | Stay start date |
| `check_out` | `date` (YYYY-MM-DD) | Yes | Stay departure date |
| `city` | `string` | No | City filter (e.g. `Addis Ababa`) |
| `region` | `string` | No | Region filter |
| `guests` | `int` | No | Number of guests (default: 2) |
| `page` | `int` | No | Page number (default: 1) |
| `page_size` | `int` | No | Results per page (default: 20, max: 50) |

---

#### `POST /v1/reservations/hold`
Acquire a short-lived (10-minute) atomic inventory hold for a room type.

**Headers:**
- `Idempotency-Key: <uuid>` (Required)

**Request Body:**
```json
{
  "property_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "room_type_id": "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed",
  "check_in": "2026-09-01",
  "check_out": "2026-09-04",
  "guest_name": "Abebe Bikila",
  "guest_phone": "+251911234567"
}
```

**Responses:**
- `201 Created`:
  ```json
  {
    "reservation_id": "c3b9b4f0-8367-4e92-bc10-74a9b3d11b33",
    "hold_token": "inv:hold:9b1deb4d:1b9d6bcd:2026-09-01",
    "expires_at": "2026-08-20T13:40:00Z",
    "status": "INVENTORY_HOLD"
  }
  ```
- `409 Conflict`: Room type sold out for one or more dates in range (`INVENTORY_UNAVAILABLE`).
- `422 Unprocessable Entity`: Invalid date range (e.g. check-out prior to check-in).

---

## 6. Testing, Linting & CI/CD

### Quality & Verification Pipeline

```bash
# Run unit tests across all domain packages
make test

# Run test suite with Go data race detector
make test-race

# Run static analysis and import boundary enforcement (depguard)
make lint

# Compile static binaries for all entrypoints
make build
```

---

### Multi-Stage Distroless Container Build

Production container images are built using a security-hardened, multi-stage distroless pattern. Timezone databases (`tzdata`) and CA certificates (`ca-certificates`) are explicitly preserved in the builder stage to support secure outbound TLS (e.g. payment webhooks, Fayda verification):

```bash
# Build production Docker image
make docker-build

# Or build manually using Docker CLI:
docker build \
  --build-arg BIN=api \
  --build-arg VERSION=1.0.0 \
  -t doki-api:1.0.0 \
  -f Dockerfile .
```

---

## 7. Roadmap & Phase Alignment

The DOKI Platform rollout follows a strict 3-month iterative phase plan:

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│                             3-MONTH PHASE ROLLOUT                                │
├────────────────────────┬────────────────────────┬────────────────────────────────┤
│   Phase 1 (Weeks 1–4)  │   Phase 2 (Weeks 5–8)  │      Phase 3 (Weeks 9–12)      │
│  Foundation & Hold     │  Booking & Payments    │  Ledger, Outbox & Release      │
├────────────────────────┼────────────────────────┼────────────────────────────────┤
│ • Clean Architecture   │ • Reservation State    │ • Double-Entry Financial       │
│   Skeleton & DI        │   Machine Engine       │   Ledger Subsystem             │
│ • Database Schema &    │ • Payment Adapters:    │ • Automated Commission         │
│   Migrations           │   Telebirr, CBE, Chapa │   Calculation Engine           │
│ • Two-Tier Redis Lua   │ • Idempotency-Key      │ • Transactional Outbox Worker  │
│   Hold Engine          │   Middleware           │   (SKIP LOCKED Polling)        │
│ • Public Property      │ • Front-Desk PMS:      │ • OpenTelemetry Tracing &      │
│   Search API           │   Check-In / Out       │   Prometheus Dashboard         │
│ • Structured slog JSON │ • Fayda Identity       │ • Concurrency Load Testing &   │
│   & Health Probes      │   Verification Adapter │   Production Release Drill     │
└────────────────────────┴────────────────────────┴────────────────────────────────┘
```

---

## 8. Contributing & Code Standards

1. **Pure Domain Boundary:** Never import external database (`pgx`), HTTP (`chi`), or cache (`redis`) packages into `internal/domain`.
2. **Context Propagation:** Every repository method, service call, and handler must accept and propagate `context.Context`.
3. **No Floats for Currency:** Always use `types.Money` (`AmountMinor int64`, `Currency string`).
4. **Structured Logging:** Use `slog` with contextual attributes (`trace_id`, `property_id`, `reservation_id`, `actor_user_id`).

---

**© 2026 DOKI Hotels Inc. All rights reserved.**
