# SPEC-07: Rate Limit System Specification

## Overview

```
pkg/ratelimit/
├── leaky_bucket.go        # Core rate limiting implementation
├── leaky_bucket_test.go  # Unit tests
└── extra_test.go         # Additional tests
```

## 1. Core Implementation

### LeakyBucket Struct

```go
type LeakyBucket struct {
    tokens chan struct{}  // Channel holding available tokens
    stop   chan struct{}  // Shutdown signal
}
```

**Algorithm:** Channel-based leaky bucket (not classic token bucket)

### Constructor

```go
func NewLeakyBucket(rate, capacity int) *LeakyBucket
```

**Parameters:**
| Parameter | Description |
|-----------|-------------|
| `rate` | Tokens added per second |
| `capacity` | Maximum tokens (burst allowance) |

**Behavior:**
1. Pre-fill channel to `capacity` with `struct{}{}` tokens
2. Start background goroutine that adds tokens at `time.Second / rate` interval
3. Non-blocking token addition (won't block if bucket full)

### Acquire

```go
func (b *LeakyBucket) Acquire(ctx context.Context) error
```

**Behavior:**
- Blocks until a token is available
- Receives from `tokens` channel (consumes token)
- Returns `ctx.Err()` if context cancelled/times out
- Non-blocking check first (try acquire without blocking)

### Close

```go
func (b *LeakyBucket) Close() error
```

**Behavior:**
- Sends signal on `stop` channel
- Closes `tokens` channel
- Returns nil (no error possible currently)

## 2. Algorithm Details

### Token Refill

```go
func (b *LeakyBucket) startRefill(rate int) {
    ticker := time.NewTicker(time.Second / time.Duration(rate))
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            select {
            case b.tokens <- struct{}{}:
                // Token added
            default:
                // Bucket full, drop token
            }
        case <-b.stop:
            return
        }
    }
}
```

### Rate Calculation

| Rate (tokens/sec) | Interval |
|-------------------|----------|
| 1 | 1s |
| 10 | 100ms |
| 100 | 10ms |
| 1000 | 1ms |

### Burst Handling

- `capacity` determines maximum burst
- If rate=100, capacity=100: can burst 100 requests instantly
- After burst, limited to rate tokens/second

## 3. Usage in Scan Orchestration

### Per-CIDR Rate Limiting

In `chunk_lifecycle.go`:
```go
func buildRuntime(chunks []task.Chunk, policy dispatchPolicy) ([]chunkRuntime, error) {
    runtimes := make([]chunkRuntime, len(chunks))
    for i, chunk := range chunks {
        runtimes[i] = chunkRuntime{
            // ...
            bkt: ratelimit.NewLeakyBucket(policy.bucketRate, policy.bucketCapacity),
        }
    }
    return runtimes, nil
}
```

### Dispatch Integration

In `task_dispatcher.go`:
```go
func dispatchTasks(ctx context.Context, runtimes []chunkRuntime, ...) error {
    for _, rt := range runtimes {
        for i := rt.tracker.NextIndex; i < rt.tracker.TotalCount; i++ {
            // Acquire token (blocks if rate limit exceeded)
            if err := rt.bkt.Acquire(ctx); err != nil {
                return err
            }
            // ... dispatch task
        }
    }
}
```

## 4. Configuration

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-bucket-rate` | 100 | Tokens per second (accepted range 1-1000000) |
| `-bucket-capacity` | 100 | Burst capacity (accepted range 1-1000000) |

### Example

```bash
# High rate for local network
go run ./cmd/port-scan scan -cidr-file ips.csv -bucket-rate 1000 -bucket-capacity 500

# Conservative rate for internet
go run ./cmd/port-scan scan -cidr-file ips.csv -bucket-rate 10 -bucket-capacity 20
```

## 5. Design Decisions

| Decision | Rationale |
|----------|-----------|
| Channel-based | Simple, idiomatic Go |
| Non-blocking refill | Won't block on full bucket |
| Per-CIDR buckets | Independent rate limits per network |
| Context-aware | Respects cancellation |

## 6. Extending Rate Limiting

### Adding Token Bucket Algorithm

```go
type TokenBucket struct {
    tokens     float64
    maxTokens  float64
    refillRate float64
    lastRefill time.Time
    mu         sync.Mutex
}

func (tb *TokenBucket) Acquire(ctx context.Context) error {
    tb.mu.Lock()
    tb.refill()
    if tb.tokens >= 1 {
        tb.tokens--
        tb.mu.Unlock()
        return nil
    }
    tb.mu.Unlock()
    // Wait and retry...
}
```

### Adding Global Rate Limit

1. Create `GlobalLeakyBucket` singleton
2. Add global acquire in `dispatchTasks()`
3. Both per-CIDR and global apply

## 7. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/ratelimit/leaky_bucket.go` | Core implementation |
| `pkg/ratelimit/leaky_bucket_test.go` | Unit tests |
| `pkg/ratelimit/extra_test.go` | Additional tests |
| `pkg/scanapp/chunk_lifecycle.go` | Bucket creation |
| `pkg/scanapp/task_dispatcher.go` | Bucket usage |

## 8. Integration Points

- **Config**: Rate and capacity from `config.Config`
- **Chunk Runtime**: Each `chunkRuntime` has its own bucket
- **Dispatcher**: Calls `Acquire()` before each task
