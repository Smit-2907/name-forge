# NameForge 🚀

NameForge is a high-performance, concurrent, production-grade startup naming and domain lookup engine. Built with **Go 1.26**, the system utilizes a channel-based worker pool model to evaluate domain availability and registry pricing across multiple TLDs concurrently. Results are scored from 0-100 based on brandability metrics, cached in **Redis** to prevent upstream rate-limits, and logged inside **PostgreSQL** for operations analytics.

---

## System Architecture

```
User Input
   ↓
[REST Controller] (Fiber Router)
   ↓
[Name Generator Orchestrator] ──► AI / Fallback Generator
                               ──► Morphological Combinatorics
                               ──► Hybrid Recombination Splicer
   ↓
[Filtering Engine] (Length, phonetics validation, vowel/consonants scanning)
   ↓
[Concurrent Worker Pool] ──► Query Redis Cache (Hit) ──► Return
                         ──► Query Porkbun / Namecheap (Miss) ──► Cache & Return
   ↓
[Ranking & Scoring Engine] (Available state, TLD priority, price offsets)
   ↓
[PostgreSQL Analytics Engine] (Asynchronous logs save, latency metrics recording)
   ↓
API JSON Response
```

---

## Directory Layout

```
├── cmd/
│   └── api/
│       └── main.go                 # Application bootstrap entrypoint
├── internal/
│   ├── config/
│   │   └── config.go               # Config struct parser (ENV variables / .env)
│   ├── models/
│   │   └── models.go               # Shared JSON representations & DB schemas
│   ├── db/
│   │   └── postgres.go             # PostgreSQL connection pool and migrations
│   ├── cache/
│   │   └── redis.go                # Redis cache helper (graceful degradation)
│   ├── generator/
│   │   ├── generator.go            # Naming orchestrator
│   │   ├── ai.go                   # OpenAI provider client + local backup
│   │   ├── morphological.go        # Physics/Nature base word combiner
│   │   └── hybrid.go               # Syllable-splicing recombination
│   ├── filters/
│   │   └── filter.go               # Consonant checks, avoids lists, brandability
│   ├── providers/
│   │   ├── provider.go             # DomainProvider interface
│   │   ├── porkbun.go              # Porkbun JSON API integration
│   │   ├── namecheap.go            # Namecheap XML API integration
│   │   └── mock.go                 # Simulated mock provider for sandboxes
│   ├── workers/
│   │   └── pool.go                 # Concurrency channels-based worker pool
│   ├── ranking/
│   │   └── ranker.go               # 0-100 composite ranking scores
│   ├── api/
│   │   ├── handler.go              # REST endpoint controllers & health check
│   │   ├── middleware.go           # Rate limiting, CORS, Zerolog, Prometheus
│   │   └── router.go               # Fiber endpoints mapper
│   └── utils/
│       └── helper.go               # Text sanitization and formatting helpers
├── web/
│   ├── index.html                  # Premium Dashboard HTML Layout
│   ├── styles.css                  # Obsidian black & glassmorphism theme stylesheet
│   └── app.js                      # UI logic and progression milestones controller
├── Dockerfile                      # Multistage production Alpine build
├── docker-compose.yml              # Local Docker environment orchestrator
├── .env.example                    # Sample configuration variables
└── go.mod                          # Go package dependency manager
```

---

## API Documentation

### 1. Naming Generator
* **Endpoint**: `POST /generate` (also accepts `/api/generate`)
* **Headers**: `Content-Type: application/json`
* **Request Payload**:
```json
{
  "description": "AI-powered BPO for customer support automation",
  "style": ["modern", "premium"],
  "themes": ["physics", "nature"],
  "tlds": [".com", ".ai", ".io"],
  "avoid": ["tech", "labs"]
}
```
* **Response Output**:
```json
{
  "results": [
    {
      "name": "Velora",
      "domain": "velora.ai",
      "available": true,
      "price": 59.99,
      "currency": "USD",
      "score": 98
    },
    {
      "name": "Fluxen",
      "domain": "fluxen.com",
      "available": true,
      "price": 9.99,
      "currency": "USD",
      "score": 95
    }
  ]
}
```

### 2. Service Health Check
* **Endpoint**: `GET /health`
* **Response Output**:
```json
{
  "status": "healthy",
  "postgres": "connected",
  "redis": "connected",
  "timestamp": "2026-05-26T17:20:00+05:30"
}
```

### 3. Database Summary Stats
* **Endpoint**: `GET /api/analytics`
* **Response Output**:
```json
{
  "total_searches": 142,
  "total_names_generated": 5260,
  "total_domain_checks": 15780,
  "availability_rate": 58.4,
  "average_search_speed_ms": 284.5
}
```

### 4. Prometheus Metrics
* **Endpoint**: `GET /metrics`
* Exposes standard Prometheus Go collector metrics along with custom counters:
  * `nameforge_http_requests_total{path, status}`: Counter for endpoint checks.
  * `nameforge_http_request_duration_seconds{path}`: Histogram for REST processing times.
  * `nameforge_domain_checks_total{tld, available, cached}`: Count of domain checks.

---

## Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `PORT` | HTTP Server port | `8080` |
| `LOG_LEVEL` | Logging level (`debug`, `info`, `warn`, `error`) | `debug` |
| `DATABASE_URL` | Postgres DSN link | `postgres://postgres:postgres@localhost:5432/nameforge?sslmode=disable` |
| `REDIS_URL` | Redis Connection URI | `redis://localhost:6379/0` |
| `USE_MOCK_PROVIDERS` | Bypass Porkbun/Namecheap with sandbox mock | `true` |
| `WORKER_COUNT` | Size of concurrent check thread pools | `30` |
| `CACHE_TTL_HOURS` | Duration domain checks are held in Redis | `24` |
| `RATE_LIMIT_MAX` | Max client requests in window | `100` |
| `RATE_LIMIT_WINDOW_MS`| Duration of rate-limit window in ms | `60000` |
| `OPENAI_API_KEY` | OpenAI completion API key | *empty* |
| `PORKBUN_API_KEY` | Porkbun Developer API Key | *empty* |
| `PORKBUN_SECRET_KEY` | Porkbun Developer Secret Key | *empty* |
| `NAMECHEAP_USERNAME` | Namecheap registered account username | *empty* |
| `NAMECHEAP_API_KEY` | Namecheap account integration Key | *empty* |
| `NAMECHEAP_CLIENT_IP`| Whitelisted developer IP for Namecheap | `127.0.0.1` |

---

## Getting Started

### Method A: Docker Compose (Recommended)
This launches PostgreSQL, Redis, and NameForge with automatic DB creation, migrations, and caching configured:
```bash
# Build and launch
docker compose up --build

# Verify health
curl http://localhost:8080/health
```
Open [http://localhost:8080](http://localhost:8080) to access the search dashboard.

### Method B: Local Setup (Without Docker)
1. **Ensure Databases are Running**:
   Start Postgres on port 5432 and Redis on port 6379 locally.
2. **Download Dependencies**:
   ```bash
   go mod download
   ```
3. **Configure Settings**:
   Edit `.env` values to match your local setup.
4. **Compile & Run**:
   ```bash
   go build -o nameforge ./cmd/api
   ./nameforge
   ```
   
---

## Production Readiness Checklist
* Set `USE_MOCK_PROVIDERS=false` and supply real Porkbun or Namecheap keys.
* Set `LOG_LEVEL=info` to reduce console write operations under high traffic.
* Scale Fiber horizontally behind a reverse proxy (e.g. Nginx or Cloudflare) which handles SSL termination.
* Configure Prometheus to scrape the `/metrics` endpoint to view rate-limits, worker pools, and cache hit metrics.
# landingpage_leadlense
