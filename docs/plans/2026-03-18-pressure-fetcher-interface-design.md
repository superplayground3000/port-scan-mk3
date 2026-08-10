# Pressure Fetcher Interface Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

**Date:** 2026-03-18  
**Status:** Approved

## Overview

Refactor `fetchPressure` from a concrete function into an interface, allowing different fetching implementations. This enables:
1. Current simple HTTP GET with JSON response (backward compatible)
2. Authenticated flow with OAuth-style token management

## Architecture

### Interface Definition

Location: `pkg/scanapp/pressure.go` (new file)

```go
type PressureFetcher interface {
    Fetch(ctx context.Context) (int, error)
}
```

### Implementations

#### 1. SimplePressureFetcher

- Constructor: `NewSimplePressureFetcher(url string, client *http.Client) PressureFetcher`
- Behavior: Makes GET request to URL, parses `{"pressure": N}` response
- Maintains current behavior

#### 2. AuthenticatedPressureFetcher

- Constructor: `NewAuthenticatedPressureFetcher(authURL, dataURL, clientID, clientSecret string, client *http.Client) PressureFetcher`
- Auth flow: POST to authURL with `application/x-www-form-urlencoded` body containing `client_id` and `client_secret`
- Auth response: `{"access_token": "...", "token_type": "Bearer", "expires_in": 3600}`
- Data flow: GET to dataURL with `Authorization: Bearer <token>` header
- Data response: Array format - extract `Percent` from first element's `data` object

### Token Caching Logic

- Store token + expiry timestamp (current time + expires_in seconds)
- Before data request: check if token expired or will expire within 30 seconds
- If expired/near-expiry: re-authenticate first
- Thread-safe with mutex

## Data Flow

```
pollPressureAPI()
    │
    ├─ accepts PressureFetcher interface
    │
    └─ calls fetcher.Fetch(ctx) → returns (int, error)
            │
            ├─ SimplePressureFetcher
            │       └─ GET url → parse {"pressure": N}
            │
            └─ AuthenticatedPressureFetcher
                    ├─ POST authURL (url-encoded client_id + client_secret)
                    ├─ parse response → extract access_token + expires_in
                    ├─ cache token with expiry timestamp
                    ├─ GET dataURL (Bearer token in header)
                    └─ parse array response → extract "Percent" from first element
```

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Auth request fails | Return error immediately |
| Auth returns non-200 | Return error with status code |
| Auth response invalid | Return error (missing access_token) |
| Data request fails | Return error immediately |
| Data response invalid | Return error (no valid entries) |
| Token expired during data fetch | Retry once with fresh token |
| Multiple consecutive failures (3x) | Send error to errCh, terminate polling |

## Configuration Changes

Add CLI flags in `pkg/config/config.go`:

| Flag | Type | Description |
|------|------|-------------|
| `-pressure-auth-url` | string | Auth endpoint URL (required for authenticated fetcher) |
| `-pressure-data-url` | string | Data endpoint URL |
| `-pressure-client-id` | string | OAuth client_id |
| `-pressure-client-secret` | string | OAuth client_secret |
| `-pressure-use-auth` | bool | Use authenticated fetcher |

**Validation:**
- If `-pressure-use-auth` is set: require all 4 auth-related flags
- If not set: use existing simple fetcher behavior

## Refactoring pollPressureAPI

### Current Signature
```go
func pollPressureAPI(ctx context.Context, cfg config.Config, opts RunOptions, ctrl *speedctrl.Controller, logger *scanLogger, errCh chan<- error)
```

### New: Pass fetcher via RunOptions
```go
type RunOptions struct {
    // ... existing fields
    PressureFetcher PressureFetcher  // NEW: injectable fetcher
}
```

### Backward Compatibility
- If `opts.PressureFetcher` is nil → construct from config (existing behavior)
- Existing tests work unchanged

## Testing Strategy

1. **Unit tests for SimplePressureFetcher**
   - Mock HTTP server returning pressure value
   - Test various response formats (int, float, string)

2. **Unit tests for AuthenticatedPressureFetcher**
   - Mock auth server + data server
   - Test token caching and refresh
   - Test error cases (auth failure, data failure, invalid response)

3. **Integration tests**
   - Full polling flow with mocked endpoints

4. **Existing tests**
   - Pass unchanged (backward compatible)

## File Changes Summary

| File | Action |
|------|--------|
| `pkg/scanapp/pressure.go` | NEW - interface + implementations |
| `pkg/scanapp/pressure_monitor.go` | MODIFY - accept interface |
| `pkg/scanapp/scan.go` | MODIFY - add PressureFetcher to RunOptions |
| `pkg/scanapp/scan_test.go` | MODIFY - may need updates if tests inject custom fetchers |
| `pkg/scanapp/scan_helpers_test.go` | MODIFY - may need updates |
| `pkg/config/config.go` | MODIFY - add auth-related flags |
| `cmd/port-scan/main.go` | MODIFY - wire up new flags |

## Out of Scope

- Changing CLI command structure
- Adding new commands
- Modifying scan output format
- Changes to other packages beyond what's listed above
