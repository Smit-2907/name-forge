# NameForge — Full Project Context

> **For AI Agents**: Read this file first before doing anything else.
> This document explains the entire project: what it is, how it works, the file structure, the tech stack, the deployment setup, and pending tasks.

---

## 1. What Is NameForge?

NameForge is a **production-grade SaaS startup naming engine** built in Go.

It does two things in one API call:
1. **Generates brand name candidates** using three AI/algorithmic methods (AI via OpenAI, Morphological combining, Hybrid recombination)
2. **Checks domain availability** across multiple TLDs (`.com`, `.io`, `.ai`, `.in`, etc.) via a concurrent worker pool, querying real registrar APIs

The frontend is a **static HTML/CSS/JS single-page app** served by the Go backend itself.

---

## 2. Tech Stack

| Layer | Technology |
|---|---|
| **Language** | Go 1.26 |
| **Web Framework** | Fiber v2 (gofiber/fiber) |
| **Database** | PostgreSQL (via `lib/pq` driver) |
| **Cache** | Redis (via `go-redis/v9`) |
| **Logging** | Zerolog (rs/zerolog) |
| **Metrics** | Prometheus (`/metrics` endpoint) |
| **Config** | godotenv (reads `.env` file or system env vars) |
| **Frontend** | Vanilla HTML + CSS + JavaScript (no framework) |
| **Containerization** | Docker + Docker Compose |

---

## 3. Project File Structure

```
engine/
├── cmd/
│   └── api/                  # Main entrypoint (main.go lives here)
├── internal/
│   ├── api/
│   │   ├── handler.go        # All HTTP request handlers (GenerateHandler, HealthCheckHandler, AnalyticsHandler)
│   │   ├── middleware.go     # Rate limiting, CORS, request logging middleware
│   │   └── router.go        # Route registration + static file serving
│   ├── cache/                # Redis cache service (TTL-based domain result caching)
│   ├── config/               # Config struct loaded from environment variables
│   ├── db/
│   │   └── postgres.go       # PostgreSQL connection, auto-migration, CRUD functions
│   ├── filters/
│   │   └── filter.go         # Name quality filtering (rejects bad names: too short, consonant-heavy, etc.)
│   ├── generator/
│   │   ├── ai.go             # OpenAI GPT-based name generation
│   │   ├── morphological.go  # Algorithmic morphological name combining
│   │   ├── hybrid.go         # Hybrid recombination of AI + morphological names
│   │   └── generator.go      # Orchestrator that runs all 3 generators concurrently
│   ├── models/               # All shared data models/structs (Request, Response, Search, DomainCheck, etc.)
│   ├── providers/
│   │   ├── provider.go       # DomainProvider interface definition
│   │   ├── mock.go           # Mock provider (used when USE_MOCK_PROVIDERS=true)
│   │   ├── godaddy.go        # GoDaddy API integration
│   │   ├── namecheap.go      # Namecheap API integration
│   │   ├── porkbun.go        # Porkbun API integration
│   │   ├── hostinger.go      # Hostinger API integration
│   │   └── bigrock.go        # BigRock API integration
│   ├── ranking/              # Composite score calculator (brand score + TLD + availability + price)
│   ├── utils/                # Input sanitization helpers
│   └── workers/              # Concurrent domain check worker pool (30 workers by default)
├── public/
│   ├── index.html            # Frontend SPA entry point
│   ├── styles.css            # All frontend CSS (dark mode, glassmorphism design)
│   └── app.js                # All frontend JavaScript (form handling, API calls, results rendering)
├── .env                      # Local development environment variables (NOT committed to git)
├── .env.example              # Template showing all required env vars
├── Dockerfile                # Multi-stage Docker build (builder: golang:1.26-alpine, runtime: alpine:3.19)
├── docker-compose.yml        # Local dev stack (Go API + PostgreSQL + Redis)
├── go.mod                    # Go module definition (module name: `nameforge`)
├── go.sum                    # Go dependency checksums
└── vercel.json               # Vercel config (currently unused, Railway is used for hosting)
```

---

## 4. How the API Works (Request Lifecycle)

### Endpoint: `POST /generate`

1. **Request Parsing** — Parses JSON body into `models.GenerateRequest` (description, style tags, themes, TLDs, avoid keywords)
2. **Validation** — Sanitizes all inputs via `utils.CleanInput()`
3. **DB Write** — Saves search metadata to `searches` table in PostgreSQL
4. **Name Generation** — Runs 3 generators concurrently via `generator.Orchestrator.GenerateNames()`:
   - `ai.go` → calls OpenAI API (falls back to local generator if `OPENAI_API_KEY` is not set)
   - `morphological.go` → algorithmic word-part combining
   - `hybrid.go` → recombines outputs from the first two
5. **Filter** → `filters/filter.go` rejects low-quality names (too short, hard to pronounce, excessive consonants, etc.)
6. **Worker Pool** → `workers/` dispatches concurrent domain check jobs across 30 workers, querying real registrar APIs
7. **Cache** → Results are cached in Redis (TTL: 24 hours) to avoid re-checking the same domains
8. **Scoring** → `ranking/` computes a composite 0–100 score per domain result
9. **Currency Conversion** → All prices displayed in INR (USD × 83.50)
10. **DB Write (async)** → Saves domain check results to `domain_checks` table in a background goroutine
11. **Response** → Returns sorted `[]models.ResultItem` (sorted by score descending)

### Other Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Returns DB + Redis connection status |
| `GET` | `/api/analytics` | Returns aggregate stats (total searches, names generated, domain checks, availability rate) |
| `GET` | `/metrics` | Prometheus metrics endpoint |
| `POST` | `/api/generate` | Alias for `/generate` |
| `GET` | `/` | Serves static frontend from `./public/` |

---

## 5. Database Schema (PostgreSQL)

Auto-migrated on startup by `internal/db/postgres.go`:

```sql
-- Stores each search request
CREATE TABLE searches (
    id          BIGSERIAL PRIMARY KEY,
    description TEXT NOT NULL,
    style       TEXT[] NOT NULL,
    themes      TEXT[] NOT NULL,
    tlds        TEXT[] NOT NULL,
    avoid       TEXT[] NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Stores each generated name candidate
CREATE TABLE generated_names (
    id              BIGSERIAL PRIMARY KEY,
    search_id       BIGINT REFERENCES searches(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    generator_type  VARCHAR(50) NOT NULL,
    score           INT NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Stores domain availability check results
CREATE TABLE domain_checks (
    id          BIGSERIAL PRIMARY KEY,
    name_id     BIGINT REFERENCES generated_names(id) ON DELETE CASCADE,
    domain      VARCHAR(255) NOT NULL,
    tld         VARCHAR(50) NOT NULL,
    available   BOOLEAN NOT NULL,
    price       DECIMAL(10,2) NOT NULL,
    currency    VARCHAR(10) NOT NULL,
    platform    VARCHAR(100) NOT NULL DEFAULT 'Unknown',
    offers      TEXT,
    checked_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Stores analytics events (e.g., search latency)
CREATE TABLE analytics_events (
    id              BIGSERIAL PRIMARY KEY,
    event_type      VARCHAR(100) NOT NULL,
    metric_value    DOUBLE PRECISION DEFAULT 0.0,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

## 6. Environment Variables

All configuration is done via environment variables. In local dev, they are loaded from `.env`.

| Variable | Description | Local Default |
|---|---|---|
| `PORT` | HTTP server port | `8080` |
| `LOG_LEVEL` | Zerolog level (`debug`, `info`, `warn`) | `debug` |
| `DATABASE_URL` | PostgreSQL DSN | `postgres://postgres:postgres@localhost:5433/nameforge?sslmode=disable` |
| `REDIS_URL` | Redis DSN | `redis://localhost:6380/0` |
| `USE_MOCK_PROVIDERS` | If `true`, uses mock domain data instead of real APIs | `true` |
| `WORKER_COUNT` | Number of concurrent domain check workers | `30` |
| `CACHE_TTL_HOURS` | Redis cache TTL for domain results | `24` |
| `RATE_LIMIT_MAX` | Max requests per window | `100` |
| `RATE_LIMIT_WINDOW_MS` | Rate limit window in milliseconds | `60000` |
| `OPENAI_API_KEY` | OpenAI key (optional; falls back to local generator if not set) | _(empty)_ |
| `PORKBUN_API_KEY` | Porkbun registrar API key | _(empty)_ |
| `PORKBUN_SECRET_KEY` | Porkbun registrar secret | _(empty)_ |
| `NAMECHEAP_USERNAME` | Namecheap account username | _(empty)_ |
| `NAMECHEAP_API_KEY` | Namecheap API key | _(empty)_ |
| `NAMECHEAP_CLIENT_IP` | Whitelisted IP for Namecheap API | `127.0.0.1` |

---

## 7. Local Development

### Prerequisites
- Docker + Docker Compose installed
- Go 1.26+ (only needed if running without Docker)

### Run with Docker (recommended)
```bash
cd /home/smitsolanki/Desktop/engine
docker compose up --build
```
This starts:
- `nameforge_api` → Go API on `localhost:8080`
- `nameforge_postgres` → PostgreSQL on `localhost:5433`
- `nameforge_redis` → Redis on `localhost:6380`

### Run without Docker
```bash
# Start Postgres and Redis separately, then:
go run ./cmd/api
```

### Access
- **Frontend**: http://localhost:8080
- **Health**: http://localhost:8080/health
- **Metrics**: http://localhost:8080/metrics
- **Analytics**: http://localhost:8080/api/analytics

---

## 8. Production Deployment

### Current Setup
| Service | Platform |
|---|---|
| **Go API + Frontend** | Railway (Docker-based) |
| **PostgreSQL** | Supabase (managed Postgres) |
| **Redis** | Railway Redis plugin |

### Live URLs
- **Production**: https://name-forge-production.up.railway.app/
- **Health Check**: https://name-forge-production.up.railway.app/health
- **GitHub Repo**: https://github.com/Smit-2907/name-forge

### Railway Environment Variables (set in Railway dashboard → your service → Variables tab)
```
DATABASE_URL=postgresql://postgres:YOUR_PASSWORD@db.jiqqrwecoeuuaprbhbde.supabase.co:5432/postgres?sslmode=require
PORT=8080
USE_MOCK_PROVIDERS=false
WORKER_COUNT=30
CACHE_TTL_HOURS=24
RATE_LIMIT_MAX=100
RATE_LIMIT_WINDOW_MS=60000
```
> **Note**: `REDIS_URL` is auto-injected by Railway's Redis plugin — do NOT set it manually.

### Supabase Details
- **Project Ref**: `jiqqrwecoeuuaprbhbde`
- **DB Host**: `db.jiqqrwecoeuuaprbhbde.supabase.co`
- **Database**: `postgres`
- **User**: `postgres`
- **Connection string format**: `postgresql://postgres:PASSWORD@db.jiqqrwecoeuuaprbhbde.supabase.co:5432/postgres?sslmode=require`
- ⚠️ Do NOT use `NEXT_PUBLIC_SUPABASE_URL` — that is the JS SDK key, not a Postgres DSN.

### Deploy Process
```bash
git add .
git commit -m "your message"
git push origin main
# Railway auto-redeploys on every push to main
```

---

## 9. Known Issues & History

| Issue | Status | Fix |
|---|---|---|
| `onclick` handlers not working on "Forge" button | ✅ Fixed | Switched to `addEventListener` in `app.js` |
| Vercel deployment showing 404 | ✅ Resolved | Moved to Railway (Vercel can't run persistent Go servers) |
| `postgres: unconfigured` in health check | ⚠️ Pending | Set `DATABASE_URL` in Railway dashboard with Supabase DSN |
| `USE_MOCK_PROVIDERS=true` in production | ⚠️ Pending | Change to `false` in Railway env vars after setting real API keys |

---

## 10. What Still Needs to Be Done

- [ ] Set `DATABASE_URL` in Railway dashboard (Supabase connection string)
- [ ] Set `USE_MOCK_PROVIDERS=false` in Railway (to use real domain registrar APIs)
- [ ] Add real API keys if available: `OPENAI_API_KEY`, `PORKBUN_API_KEY`, `NAMECHEAP_API_KEY`
- [ ] Verify `/health` returns `{"status":"ok","postgres":"connected","redis":"connected"}`
- [ ] Test `/generate` endpoint end-to-end on production

---

## 11. Key Design Decisions

1. **Why Railway over Vercel?** — Go needs a persistent runtime for its worker pool and goroutines. Vercel only supports serverless functions and cannot run a long-lived Go HTTP server.
2. **Why Supabase for Postgres?** — Managed, free-tier Postgres with a simple connection string. Railway's own Postgres plugin works too.
3. **Why static frontend served by Go?** — Avoids CORS complexity. The Go server serves the HTML/JS/CSS from `./public/` directly, so API calls are same-origin.
4. **Why `USE_MOCK_PROVIDERS`?** — Real registrar APIs (GoDaddy, Namecheap, Porkbun) require paid API keys. Mocks let the app run and demonstrate functionality without keys.
5. **Currency in INR** — The app converts all prices to INR (rate: 1 USD = ₹83.50) for Indian market display.
