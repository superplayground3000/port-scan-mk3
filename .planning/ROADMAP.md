# Roadmap: Pressure API Evolution

## Phase 1: Add Authenticated Pressure API CLI Flags

**Goal**: Add CLI flags for authenticated pressure API configuration

### Requirements

- [ ] **AUTH-01**: Add CLI flags for authenticated pressure API

### Success Criteria

1. New flags available:
   - `-pressure-auth-url` (auth endpoint URL)
   - `-pressure-auth-client-id` (OAuth client ID)
   - `-pressure-auth-client-secret` (OAuth client secret)

### Tasks

1. Add fields to `config.Config`:
   - `PressureAuthURL`
   - `PressureAuthClientID`
   - `PressureAuthClientSecret`

2. Add flag definitions in `config.Parse()`

3. Add validation (flags optional, no validation needed)

---

## Phase 2: Wire AuthenticatedPressureFetcher in Run

**Goal**: Use authenticated fetcher when auth flags provided

### Requirements

- [ ] **AUTH-02**: Wire AuthenticatedPressureFetcher in scanapp.Run

### Success Criteria

1. When auth flags provided → use AuthenticatedPressureFetcher
2. When only basic pressure-api → use SimplePressureFetcher

### Tasks

1. In `scanapp.Run()`, check if auth flags provided
2. If auth flags present, create AuthenticatedPressureFetcher
3. Otherwise, use SimplePressureFetcher (existing behavior)

## Phase 3: Multi-Source Pressure Inputs and Cross-Platform Run Scripts

**Goal**: Expand pressure control from single source to multiple sources and provide runnable examples for Linux/Windows

**Plans:** 2 plans

Plans:
- [ ] 03-01-PLAN.md — Deliver PRESSURE-03 by adding multi-source config contract, aggregation policy, and runtime wiring.
- [ ] 03-02-PLAN.md — Deliver OPS-01 by adding Linux/Windows runnable scripts plus verification/doc updates.

### Requirements

- [ ] **PRESSURE-03**: Support multiple pressure sources in one scan run
- [ ] **OPS-01**: Provide example run scripts for Linux Bash and Windows BAT

### Success Criteria

1. CLI can receive multiple pressure source definitions in a single run.
2. Runtime can evaluate pressure from multiple sources with explicit selection policy.
3. Existing single-source behavior remains backward compatible.
4. Example scripts are provided and runnable on Linux (`.sh`) and Windows (`.bat`).

### Tasks

1. Extend config/CLI contract to accept multiple pressure source inputs.
2. Refactor pressure fetch path in `pkg/scanapp` to aggregate multiple source fetchers.
3. Define policy for combining multiple pressure readings (pause/resume decisions).
4. Add cross-platform sample scripts demonstrating multi-source configuration.

---

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| AUTH-01 | Phase 1 | Pending |
| AUTH-02 | Phase 2 | Pending |
| PRESSURE-03 | Phase 3 | Pending |
| OPS-01 | Phase 3 | Pending |

**Coverage:**
- v1 requirements: 2 total
- v2 requirements: 2 total
- Mapped to phases: 4
- Unmapped: 0 ✓
