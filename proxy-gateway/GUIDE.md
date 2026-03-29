# Proxy Gateway — Go Framework Guide

A composable, multi-protocol proxy gateway framework written in Go. It routes client requests through pools of upstream proxies with pluggable authentication, session affinity, rate limiting, MITM interception, and TLS fingerprint spoofing.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Core Concepts](#core-concepts)
   - [Gateway](#gateway)
   - [Handler Pipeline](#handler-pipeline)
   - [Downstream (Listeners)](#downstream-listeners)
   - [Upstream (Dialers)](#upstream-dialers)
   - [Request & Result](#request--result)
   - [Context Values](#context-values)
3. [Middleware](#middleware)
   - [Auth](#auth)
   - [Session Affinity & SessionSeed](#session-affinity--sessionseed)
   - [Rate Limiting](#rate-limiting)
   - [MITM Interception](#mitm-interception)
   - [TLS Fingerprint Spoofing](#tls-fingerprint-spoofing)
4. [Proxy Sources](#proxy-sources)
   - [Static File](#static-file)
   - [Bottingtools](#bottingtools)
   - [Geonode](#geonode)
   - [Writing Your Own Source](#writing-your-own-source)
5. [Utilities](#utilities)
   - [CountingPool](#countingpool)
   - [MapAuth](#mapauth)
   - [Proxy Format Parsing](#proxy-format-parsing)
   - [Meta Context](#meta-context)
6. [The Server Binary](#the-server-binary)
   - [Configuration](#configuration)
   - [Username Format](#username-format)
   - [Admin API](#admin-api)
   - [Pipeline Assembly](#pipeline-assembly)
7. [Data Flow Walkthrough](#data-flow-walkthrough)
8. [Package Layout](#package-layout)

---

## Architecture Overview

```
                          ┌─────────────────────────────────────────────┐
                          │              Gateway                        │
                          │                                             │
Client ──HTTP CONNECT──→  │  HTTPDownstream ─┐                          │
Client ──Plain HTTP───→   │                  ├─→ Handler Pipeline ──→ Upstream ──→ Target
Client ──SOCKS5──────→    │  SOCKS5Downstream┘   (Auth → Session       │
                          │                       → RateLimit → Source) │
                          └─────────────────────────────────────────────┘
```

The framework separates concerns into three layers:

- **Downstream** — accepts client connections (HTTP proxy, SOCKS5)
- **Handler Pipeline** — a chain of middleware that resolves each request to an upstream proxy
- **Upstream** — dials the target through the resolved proxy (HTTP CONNECT or SOCKS5)

Every layer is defined by a Go interface, making each piece independently testable and replaceable.

---

## Core Concepts

All core types live in `proxy-gateway/core/`.

### Gateway

`Gateway` is the top-level orchestrator. It wires multiple downstream listeners to a single handler pipeline:

```go
gw := core.New(pipeline,
    core.Listen(&core.HTTPDownstream{}, ":8100"),
    core.Listen(&core.SOCKS5Downstream{}, ":1080"),
    core.WithUpstream(core.AutoUpstream()),
)
gw.ListenAndServe()
```

**Options:**
| Function | Purpose |
|---|---|
| `Listen(downstream, addr)` | Add a listener on `addr` using the given downstream protocol |
| `WithUpstream(u)` | Set the upstream dialer (default: `AutoUpstream()`) |

The gateway injects the `Upstream` into any `Downstream` that implements `UpstreamAware`, then starts all listeners concurrently. It blocks until the first listener returns an error.

### Handler Pipeline

The central abstraction. Every middleware and proxy source implements:

```go
type Handler interface {
    Resolve(ctx context.Context, req *Request) (*Result, error)
}
```

Handlers compose like HTTP middleware — each one does its work, then calls `next.Resolve(ctx, req)`:

```go
// Simplest possible handler
core.HandlerFunc(func(ctx context.Context, req *core.Request) (*core.Result, error) {
    return core.Resolved(&core.Proxy{Host: "1.2.3.4", Port: 8080}), nil
})
```

### Downstream (Listeners)

A `Downstream` accepts client connections and dispatches them through the handler:

```go
type Downstream interface {
    Serve(addr string, handler Handler) error
}
```

Built-in implementations:

| Type | Protocol | Details |
|---|---|---|
| `HTTPDownstream` | HTTP proxy | Handles both `CONNECT` (tunneling) and plain HTTP forwarding. Extracts `Proxy-Authorization` Basic auth. |
| `SOCKS5Downstream` | SOCKS5 | Full RFC 1928 + RFC 1929 implementation. Username/password auth or no-auth. |

Both implement `UpstreamAware` so the `Gateway` can inject the upstream dialer.

Convenience functions are also provided for standalone use:

```go
core.ListenHTTP(":8100", handler)    // HTTP proxy
core.ListenSOCKS5(":1080", handler)  // SOCKS5
```

### Upstream (Dialers)

An `Upstream` dials a target host through an upstream proxy:

```go
type Upstream interface {
    Dial(ctx context.Context, proxy *Proxy, target string) (net.Conn, error)
}
```

Built-in implementations:

| Type | Protocol | How it works |
|---|---|---|
| `HTTPUpstream` | HTTP CONNECT | Sends `CONNECT target HTTP/1.1` with optional Basic auth |
| `SOCKS5Upstream` | SOCKS5 | Uses `golang.org/x/net/proxy` (RFC 1928/1929) |
| `AutoUpstream()` | Auto-detect | Dispatches to HTTP or SOCKS5 based on `proxy.Protocol` |

`AutoUpstream()` is the default when no upstream is explicitly configured.

### Request & Result

**`Request`** carries everything the downstream extracted from the client:

```go
type Request struct {
    RawUsername  string        // From Basic auth or SOCKS5 handshake
    RawPassword  string        // From Basic auth or SOCKS5 handshake
    Target       string        // Destination host:port
    Conn         net.Conn      // Raw client connection (CONNECT/SOCKS5)
    HTTPRequest  *http.Request // Decoded HTTP request (plain HTTP or MITM)
}
```

**`Result`** carries the pipeline's decision:

```go
type Result struct {
    Proxy        *Proxy        // Upstream proxy to use (nil = handled/rejected)
    ConnTracker  ConnTracker   // Tracks bytes/connections for rate limiting
    ResponseHook func(*http.Response) *http.Response  // Modify response before sending
    HTTPResponse *http.Response // Synthetic response (blocking, caching)
    UpstreamConn net.Conn      // Pre-dialed connection (MITM)
}
```

The `Proxy` struct is the resolved upstream endpoint:

```go
type Proxy struct {
    Host     string
    Port     uint16
    Username string
    Password string
    Protocol Protocol  // "http" or "socks5"
}
```

### Context Values

The framework uses typed context keys to pass data between pipeline stages:

| Function | Purpose |
|---|---|
| `WithIdentity(ctx, id)` / `Identity(ctx)` | Caller's identity (set by auth parsing) |
| `WithCredential(ctx, cred)` / `Credential(ctx)` | Caller's credential (password/token) |
| `WithSessionSeed(ctx, seed)` / `GetSessionSeed(ctx)` | Deterministic seed for source decisions (set by Session middleware) |
| `WithTLSState(ctx, state)` / `GetTLSState(ctx)` | TLS interception state (MITM) |

These are the framework's own context keys. The server binary adds its own domain-specific keys on top (set, session TTL, metadata).

---

## Middleware

### Auth

Validates `Identity(ctx)` and `Credential(ctx)` against an `Authenticator`:

```go
type Authenticator interface {
    Authenticate(identity, credential string) error
}

pipeline := core.Auth(myAuthenticator, next)
```

The framework provides `utils.MapAuth` for simple username/password maps, but you can implement any auth backend (database, JWT, LDAP, etc.).

### Session Affinity & SessionSeed

`Session` pins requests with the same key to the same upstream proxy for a configurable TTL. It also manages a **`SessionSeed`** — a deterministic value that flows through context to sources, giving them a stable basis for all random-looking choices.

```go
sessions := core.Session(keyFunc, next)
```

The `KeyFunc` extracts a `SessionParams` from each request's context:

```go
type SessionParams struct {
    Key string         // Stable affinity key
    TTL time.Duration  // How long to pin (0 = no affinity)
}
```

#### How it works

- **TTL > 0** — Session computes a `*SessionSeed` from `hash(key + rotation)`, stores it in context via `WithSessionSeed`, then calls the next handler. The result is cached for the TTL duration. On cache hit, the cached proxy is returned directly without calling downstream.
- **TTL = 0 or empty key** — No affinity. The request passes straight through with **no seed in context** (`GetSessionSeed` returns `nil`). Sources decide what `nil` means for their domain.

#### SessionSeed

`SessionSeed` is a pointer type (`*SessionSeed`). Its presence or absence in context is the signal:

| Context state | Meaning | Source behavior |
|---|---|---|
| `nil` | No session affinity | Source decides: randomize per-request, refuse, etc. |
| non-`nil` | Active session | Source uses seed for deterministic choices |

```go
type SessionSeed struct { /* opaque uint64 */ }

// Deterministic index in [0, n)
func (s *SessionSeed) Pick(n int) int

// Deterministic string from a charset (session IDs, tokens, etc.)
func (s *SessionSeed) DeriveStringKey(charset string, length int) string

// Raw value for custom derivations
func (s *SessionSeed) Value() uint64
```

`DeriveStringKey` generates a deterministic string of any length from any character set:

```go
seed.DeriveStringKey("0123456789abcdef", 16)           // → "a3f7c1e9b2d04856"
seed.DeriveStringKey("abcdefghijklmnopqrstuvwxyz", 8)  // → "kpmfxqbt"
```

#### Why this matters

Many upstream proxy providers use session IDs embedded in the username to control IP stickiness (e.g., Bottingtools: `user_session-abc123`, Geonode: `user-session-abc123`). Without SessionSeed, the gateway had two disconnected mechanisms:

1. `Session` middleware caching the resolved `Proxy` (works for static pools)
2. Providers generating random session IDs on every `Resolve()` call

With SessionSeed, these unify: the session middleware owns the lifecycle (TTL, rotation counter), and providers derive everything from the seed. Same seed → same session ID, same country pick, same pool selection. `ForceRotate` bumps the rotation counter → new seed → all derived values change naturally.

#### Force rotation

```go
sessions.ForceRotate(ctx, key)  // Bumps rotation counter → new seed → new proxy
```

The rotation counter increments, producing `hash(key + newRotation)` — a completely different seed. The next `Resolve` call re-evaluates everything with the new seed.

#### Introspection

```go
sessions.GetSession(key)      // SessionInfo with seed value and rotation counter
sessions.ListSessions()       // All active sessions
```

`SessionInfo` includes `Seed` (the raw uint64) and `Rotation` (how many times it was rotated) for debugging.

Expired sessions are automatically cleaned up every 60 seconds.

### Rate Limiting

`RateLimit` enforces per-key limits on connections and bandwidth:

```go
limiter := core.RateLimit(
    func(ctx context.Context) string { return core.Identity(ctx) },
    next,
    core.StaticLimits([]core.RateLimitRule{
        {Type: core.LimitConcurrentConnections, Timeframe: core.Realtime, Max: 100},
        {Type: core.LimitTotalBytes, Timeframe: core.Daily, Max: 10 * 1024 * 1024 * 1024},
    }),
)
```

**Limit types:**

| Type | Description |
|---|---|
| `LimitConcurrentConnections` | Max simultaneous open connections |
| `LimitTotalConnections` | Total connections in a time window |
| `LimitUploadBytes` | Upload bandwidth cap |
| `LimitDownloadBytes` | Download bandwidth cap |
| `LimitTotalBytes` | Combined bandwidth cap |

**Timeframes:** `Realtime`, `Secondly`, `Minutely`, `Hourly`, `Daily`, `Weekly`, `Monthly`

The `Window` multiplier allows custom durations (e.g., `Hourly` with `Window: 6` = 6-hour rolling window).

Traffic counting is integrated via `ConnTracker` — a per-connection interface that gets `RecordTraffic` calls on every read, with a `cancel` function to kill the connection when limits are exceeded:

```go
type ConnTracker interface {
    RecordTraffic(upstream bool, delta int64, cancel func())
    Close(sentTotal, receivedTotal int64)
}
```

Multiple trackers can be chained with `ChainTrackers(a, b)`.

### MITM Interception

`MITM` terminates the client's TLS inside a CONNECT tunnel, allowing full HTTP request/response inspection and modification:

```go
ca, _ := core.NewCA()
pipeline := core.MITM(certProvider, interceptor, inner)

// Or the quick shortcut:
pipeline := core.QuickMITM(ca, upstream, inner)
```

**Key components:**

| Interface | Purpose |
|---|---|
| `CertProvider` | Provides TLS certificates for each hostname |
| `Interceptor` | Performs the actual upstream request |

**Built-in cert providers:**
- `ForgedCertProvider` — generates per-host certificates signed by your CA, with an LRU cache
- `StaticCertProvider` — uses a single certificate for all hosts

**Built-in interceptors:**
- `StandardInterceptor` — dials upstream via the resolved proxy, does a real TLS handshake to the target, and forwards the decrypted request

The MITM flow:
1. Client sends `CONNECT example.com:443`
2. Pipeline resolves to an upstream proxy
3. MITM accepts the client's TLS using a forged cert for `example.com`
4. Client sends decrypted HTTP requests over the TLS connection
5. Interceptor forwards each request through the upstream proxy to the target
6. Responses flow back through the same path

### TLS Fingerprint Spoofing

Builds on MITM to make upstream connections look like a real browser:

```go
ca, _ := core.NewCA()
pipeline := utils.TLSFingerprintSpoofing(ca, "chrome-latest", inner)
```

This uses [httpcloak](https://github.com/sardanioss/httpcloak) to spoof the TLS fingerprint. The `preset` controls which browser to impersonate (`"chrome-latest"`, `"firefox-latest"`, `"safari-latest"`).

Under the hood, it's just a custom `Interceptor` plugged into `core.MITM`.

---

## Proxy Sources

A proxy source is just a `Handler` that returns a `Result` with a `Proxy`. Sources receive a `*SessionSeed` via context — they use it for deterministic choices when non-nil, and fall back to their own randomization when nil.

The framework ships three sources in `proxy-gateway/utils/`:

### Static File

Loads proxies from a text file at startup, serves them via `CountingPool` (least-used rotation):

```go
source, _ := utils.LoadStaticFileSource("proxies.txt", utils.ProxyFormatHostPortUserPass)
```

**Seed behavior:**
- `nil` seed → least-used with random tie-breaking (a different proxy on each request)
- non-nil seed → least-used with deterministic tie-breaking (same seed picks same proxy among equally-used entries)

Supports multiple line formats:
- `host:port:user:pass` (default)
- `user:pass@host:port`
- `user:pass:host:port`

Comments (`#`) and blank lines are ignored.

### Bottingtools

Connects to the Bottingtools proxy API. Supports residential (low/high quality), ISP, and datacenter products with country targeting.

**Seed behavior:**
- `nil` seed → random session ID and random country pick on every `Resolve()`
- non-nil seed → deterministic session ID (`seed.DeriveStringKey(hex, 16)`) and deterministic country pick (`seed.Pick(len(countries))`)

```toml
[[proxy_set]]
name = "residential"
source_type = "bottingtools"

[proxy_set.bottingtools]
username = "myuser"
password_env = "BT_PASSWORD"
host = "resi.bottingtools.io"

[proxy_set.bottingtools.product]
type = "residential"
quality = "high"
countries = ["US", "DE"]
```

### Geonode

Connects to Geonode's proxy gateway. Supports rotating and sticky sessions, HTTP and SOCKS5, multiple gateway locations (FR, US, SG), and country targeting.

**Seed behavior:** same as Bottingtools — deterministic session ID and country with a seed, random without.

```toml
[[proxy_set]]
name = "geonode_resi"
source_type = "geonode"

[proxy_set.geonode]
username = "myuser"
password_env = "GN_PASSWORD"
gateway = "fr"
protocol = "http"
countries = ["US"]

[proxy_set.geonode.session]
type = "rotating"
```

### Writing Your Own Source

Implement `core.Handler` and read the seed from context:

```go
type MySource struct { /* ... */ }

func (s *MySource) Resolve(ctx context.Context, req *core.Request) (*core.Result, error) {
    seed := core.GetSessionSeed(ctx)

    // Pick a country — deterministic with seed, random without
    country := "US"
    if seed != nil {
        country = countries[seed.Pick(len(countries))]
    } else {
        country = countries[rand.Intn(len(countries))]
    }

    // Build a session ID — deterministic with seed, random without
    sessionID := randomHex(16)
    if seed != nil {
        sessionID = seed.DeriveStringKey("0123456789abcdef", 16)
    }

    proxy := &core.Proxy{
        Host: "proxy.example.com", Port: 8080,
        Username: fmt.Sprintf("user_session-%s_country-%s", sessionID, country),
        Password: "pass",
        Protocol: core.ProtocolHTTP,
    }
    return core.Resolved(proxy), nil
}
```

The pattern is always the same: check for `nil`, branch into deterministic vs. random. The `utils` package provides shared helpers `pickCountry` and `deriveSessionID` that encapsulate this pattern for the built-in sources.

---

## Utilities

### CountingPool

A generic, lock-free pool with least-used selection:

```go
pool := utils.NewCountingPool([]core.Proxy{p1, p2, p3})

proxy := pool.Next()                // Least-used, random tie-break
proxy := pool.NextWithSeed(seed)    // Least-used, seed-deterministic tie-break (nil seed = random)
proxy := pool.NextExcluding(fn)     // Exclude entries (e.g., a failed proxy)
```

Uses atomic counters — no mutex contention. When multiple entries have the same use count, the tie is broken by either the seed (deterministic) or `CheapRandom` (non-deterministic). `NextWithSeed(nil)` is equivalent to `Next()`.

### MapAuth

Simple in-memory authenticator:

```go
auth := utils.NewMapAuth(map[string]string{
    "alice": "password1",
    "bob":   "password2",
})
```

Implements `core.Authenticator`.

### Proxy Format Parsing

Parse proxy strings in various formats:

```go
proxy, err := utils.ParseProxyLine("1.2.3.4:8080:user:pass", utils.ProxyFormatHostPortUserPass)
proxy, err := utils.ParseProxyLine("user:pass@1.2.3.4:8080", utils.ProxyFormatUserPassAtHostPort)
proxy, err := utils.ParseProxyLine("user:pass:1.2.3.4:8080", utils.ProxyFormatUserPassHostPort)
```

Handles IPv6 (bracketed notation), optional credentials, and protocol prefixes (`http://`, `socks5://`).

### Meta Context

A flat key-value map that callers can attach to request contexts:

```go
ctx = utils.WithMeta(ctx, utils.Meta{"app": "crawler", "tier": "premium"})
meta := utils.GetMeta(ctx)
app := meta.GetString("app")  // "crawler"
```

Used by proxy sources (e.g., Bottingtools) to customize upstream parameters per-request.

---

## The Server Binary

`proxy-gateway/cmd/proxy-gateway-server/` is the production server that wires the framework into a deployable binary.

### Configuration

Supports TOML, YAML, and JSON. Example `config.toml`:

```toml
bind_addr   = "127.0.0.1:8100"    # HTTP proxy listener
socks5_addr = "127.0.0.1:1080"    # SOCKS5 listener (optional)
admin_addr  = "127.0.0.1:9090"    # Admin API listener (optional)
log_level   = "info"

# Auth — single user shorthand
auth_sub      = "alice"
auth_password = "s3cret"

# Auth — multi-user (takes precedence over auth_sub/auth_password)
[users]
alice = "password1"
bob   = "password2"

# Proxy sets
[[proxy_set]]
name = "residential"
source_type = "static_file"
[proxy_set.static_file]
proxies_file = "proxies/residential.txt"
format = "host_port_user_pass"

[[proxy_set]]
name = "datacenter"
source_type = "bottingtools"
[proxy_set.bottingtools]
username = "myuser"
password_env = "BT_PASSWORD"
host = "resi.bottingtools.io"
[proxy_set.bottingtools.product]
type = "datacenter"
countries = ["US"]
```

**Environment variables:**

| Variable | Description |
|---|---|
| `LOG_LEVEL` | Override log level (`debug`, `info`, `warn`, `error`) |
| `API_KEY` | Bearer token for admin API (required to enable it) |

### Username Format

The `Proxy-Authorization` username is a **JSON object**:

```json
{"sub": "alice", "set": "residential", "minutes": 5, "meta": {"app": "crawler"}}
```

| Field | Type | Description |
|---|---|---|
| `sub` | string | User identity (must match a configured user) |
| `set` | string | Proxy set name (must match a `[[proxy_set]]` name) |
| `minutes` | int | Session TTL in minutes (0 = rotate every request) |
| `meta` | object | Arbitrary metadata passed to proxy sources |

The password in `Proxy-Authorization` is the user's configured password.

```bash
curl -x http://127.0.0.1:8100 \
  --proxy-user '{"sub":"alice","set":"residential","minutes":5,"meta":{}}:s3cret' \
  https://httpbin.org/ip
```

The session affinity key is derived from `sub` + `set` only — changing `minutes` does not create a new session.

### Admin API

When `API_KEY` and `admin_addr` are configured, a REST API is available:

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/sessions` | List all active sessions |
| `GET` | `/api/sessions/{key}` | Get a specific session |
| `POST` | `/api/sessions/{key}/rotate` | Force-rotate a session to a new proxy |

All endpoints require `Authorization: Bearer <API_KEY>`.

Session responses include `seed` (uint64) and `rotation` (counter) for debugging.

### Pipeline Assembly

The server builds this pipeline:

```
ParseJSONCreds → Auth → Session → Router → Source
```

1. **ParseJSONCreds** — parses the JSON username, populates context with `Identity`, `Credential`, `Set`, `SessionTTL`, `SessionKey`, and `Meta`
2. **Auth** — validates `Identity` + `Credential` against the configured users
3. **Session** — sticky session affinity keyed by `sub + "\x00" + set`. Sets `SessionSeed` in context when TTL > 0, leaves it nil when TTL = 0
4. **Router** — dispatches to the correct proxy source based on `set`
5. **Source** — reads `SessionSeed` from context for deterministic choices, falls back to random when nil

---

## Data Flow Walkthrough

Here's what happens when a client sends `CONNECT example.com:443`:

```
1. Client connects to :8100 (HTTP) or :1080 (SOCKS5)

2. Downstream extracts:
   - RawUsername, RawPassword (from Proxy-Authorization or SOCKS5 auth)
   - Target ("example.com:443")
   - Conn (the raw TCP connection)

3. Handler pipeline processes the Request:
   a. ParseJSONCreds: parses JSON username → sets ctx values
   b. Auth: validates Identity + Credential → rejects if invalid
   c. Session: looks up sticky entry for "alice\x00residential"
      - HIT:  returns cached Proxy
      - MISS (TTL > 0): computes SessionSeed, stores in ctx, calls next →
      - MISS (TTL = 0): no seed, calls next directly →
   d. Router: dispatches to "residential" source
   e. Source: reads GetSessionSeed(ctx)
      - non-nil: deterministic session ID, country, pool pick from seed
      - nil:     random session ID, country, pool pick
      → returns Result{Proxy: ...}

4. Downstream receives the Result:
   - Dials upstream proxy via Upstream.Dial() (HTTP CONNECT or SOCKS5)
   - Gets a net.Conn to the target through the upstream proxy

5. relay() bidirectionally copies bytes:
   - client → upstream (with optional ConnTracker counting)
   - upstream → client
   - Uses io.Copy with CloseWrite for clean TCP shutdown

6. ConnTracker.Close() records final byte counts
```

For **plain HTTP** requests (non-CONNECT), step 4 uses `ForwardPlainHTTP` instead of relay, which leverages `net/http.Transport` for proper streaming with hop-by-hop header stripping.

---

## Package Layout

```
proxy-gateway/
├── core/                          # Framework core — all interfaces & middleware
│   ├── transport.go               # Downstream, Upstream, UpstreamAware interfaces
│   ├── handler.go                 # Handler, Request, Result
│   ├── proxy.go                   # Proxy struct
│   ├── protocol.go                # Protocol type (http, socks5)
│   ├── context.go                 # Context keys (Identity, Credential, TLSState)
│   ├── session_seed.go            # SessionSeed type, DeriveStringKey, Pick, context helpers
│   ├── gateway.go                 # Gateway orchestrator
│   ├── http_downstream.go         # HTTP proxy listener (CONNECT + plain)
│   ├── socks5_downstream.go       # SOCKS5 listener
│   ├── http_upstream.go           # HTTP CONNECT dialer
│   ├── socks5_upstream.go         # SOCKS5 dialer
│   ├── upstream.go                # AutoUpstream (protocol dispatcher)
│   ├── auth.go                    # Auth middleware
│   ├── session.go                 # Session affinity middleware (manages SessionSeed lifecycle)
│   ├── ratelimit.go               # Rate limiting middleware (rolling windows)
│   ├── conn_tracker.go            # Connection lifecycle tracking
│   ├── mitm.go                    # MITM TLS interception
│   ├── forward.go                 # Plain HTTP forwarding
│   ├── relay.go                   # Bidirectional byte relay with counting
│   └── net_helpers.go             # hostPort helper (IPv6-aware)
│
├── utils/                         # Reusable utilities & proxy source implementations
│   ├── auth_map.go                # MapAuth (map-based authenticator)
│   ├── counting_pool.go           # CountingPool (least-used generic pool, seed-aware)
│   ├── seed_helpers.go            # Shared nil-seed helpers (pickCountry, deriveSessionID)
│   ├── proxy_format.go            # Proxy line parsing (multiple formats)
│   ├── meta.go                    # Meta context (arbitrary request metadata)
│   ├── country.go                 # Country code type
│   ├── cheap_random.go            # Fast xorshift64 RNG
│   ├── tls_fingerprint_spoofing.go # TLS fingerprint spoofing via httpcloak
│   ├── provider_static_file.go    # Static file proxy source (seed-aware pool selection)
│   ├── provider_bottingtools.go   # Bottingtools proxy source (seed-aware session IDs)
│   └── provider_geonode.go        # Geonode proxy source (seed-aware session IDs)
│
└── cmd/proxy-gateway-server/      # Production server binary
    ├── main.go                    # Entry point, logging, config loading
    ├── config.go                  # Config struct, TOML/YAML/JSON loader
    ├── pipeline.go                # Pipeline assembly (BuildServer)
    ├── server.go                  # HTTP/SOCKS5/Admin server startup
    ├── api.go                     # Admin REST API handlers
    ├── context.go                 # Server-specific context keys (set, TTL)
    ├── sources.go                 # Source factory (config → Handler)
    └── parse_json_creds.go        # JSON username parsing middleware
```

### Design Principles

- **Interfaces over implementations** — `Handler`, `Downstream`, `Upstream`, `Authenticator`, `ConnTracker`, `CertProvider`, `Interceptor` are all interfaces. Swap any piece without touching the rest.
- **Context is the data bus** — middleware communicates via typed context values, not struct fields. This keeps the `Handler` signature universal.
- **nil means "not applicable"** — `SessionSeed` is a pointer. The Session middleware sets it when there's affinity; sources check for nil and decide their own fallback. No sentinel values, no "is this a real seed or a fake one" ambiguity.
- **Middleware composes** — `Auth(auth, Session(keyFn, RateLimit(keyFn, source)))`. Each middleware is a handler wrapping another handler.
- **Framework vs. server** — `core/` and `utils/` are a reusable library. `cmd/proxy-gateway-server/` is one opinionated wiring of that library. You can build a completely different server using the same core.
