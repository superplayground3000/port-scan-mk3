# SPEC-11: State System Specification

## Overview

```
pkg/state/
├── state.go            # State persistence
├── state_test.go      # Unit tests
├── signal.go          # Signal handling
└── state_extra_test.go # Additional tests
```

## 1. State Persistence (state.go)

### Save Function

```go
func Save(path string, data interface{}) error
```

**Parameters:**
- `path` - File path to save JSON
- `data` - Struct to serialize

**Behavior:**
1. Create a uniquely named temp file in the destination's own directory
   (`<destination>.tmp-<random>`) — same directory, because a rename across
   filesystems is atomic nowhere and may fail outright
2. Write the JSON, `Sync`, and close the temp file
3. Replace the final path with the temp file via `os.Rename`
   (`MoveFileEx` with `REPLACE_EXISTING` on Windows)

**What this guarantees on every platform:**
- The destination is never truncated or written in place.
- Any failure before the rename removes the temp file and returns an error that
  names the failed stage (`create` / `mode` / `write` / `sync` / `close` /
  `replace`) and wraps the underlying cause, leaving an existing snapshot
  byte-for-byte intact — it is the only copy of the resume state. If removing
  the temp file also fails, that error is joined to the first one so the
  abandoned file is named rather than left unexplained.
- The rename either replaces the destination or fails loudly with the previous
  snapshot still in place.
- A replacement keeps the permissions of the file it replaced.

**What it does not guarantee:** the rename step itself is atomic only on POSIX.
Go's `os.Rename` documentation states that "even within the same directory, on
non-Unix platforms Rename is not an atomic operation", so a crash *during* the
rename is out of scope on Windows. Nothing here is durable against power loss
either: the parent directory is not fsynced after the rename.

### Load Function

```go
func Load(path string, dest interface{}) error
```

**Parameters:**
- `path` - File path to load from
- `dest` - Struct to unmarshal into

**Behavior:**
1. Read file contents
2. Unmarshal JSON into `dest`
3. Return error if file doesn't exist

### Chunk State Structure

```go
type Chunk struct {
    CIDR         string `json:"cidr"`
    NextIndex    int    `json:"nextIndex"`
    ScannedCount int    `json:"scannedCount"`
    Status       string `json:"status"`  // "pending", "scanning", "completed"
}
```

### Resume State Structure

```go
type ResumeState struct {
    Chunks   []Chunk `json:"chunks"`
    Generated string `json:"generated"`  // ISO timestamp
}
```

## 2. Signal Handling (signal.go)

### WithSIGINTCancel

```go
func WithSIGINTCancel(ctx context.Context) (context.Context, func())
```

**Behavior:**
1. Create channel for SIGINT signals
2. Install signal handler
3. Return context that cancels on SIGINT
4. Return cleanup function to restore handler

**Usage:**
```go
ctx, cancel := state.WithSIGINTCancel(context.Background())
defer cancel()

// If user presses Ctrl+C:
// - ctx is canceled
// - context.Canceled error returned by blocking operations
```

### Integration with Scan

```go
func runScan(...) error {
    ctx, cancel := state.WithSIGINTCancel(context.Background())
    defer cancel()
    
    return scanapp.Run(ctx, cfg, stdout, stderr, opts)
}
```

## 3. Resume Flow

### Save Conditions

Resume state is saved when:
1. Scan is canceled (SIGINT)
2. Runtime error occurs
3. Dispatch error (if context canceled/deadline exceeded)
4. AND scan is incomplete (some chunks not done)

### Save on Error

```go
func persistResumeState(runtimes []chunkRuntime, resumePath string, shouldSave bool) error {
    if !shouldSave {
        return nil
    }
    
    // Check if incomplete
    incomplete := false
    for _, rt := range runtimes {
        if rt.tracker.Status != "completed" {
            incomplete = true
            break
        }
    }
    
    if !incomplete {
        return nil  // Don't save if complete
    }
    
    // Collect chunk states
    chunks := make([]state.Chunk, len(runtimes))
    for i, rt := range runtimes {
        chunks[i] = state.Chunk{
            CIDR:         rt.targets[0].ipCidr.String(),
            NextIndex:    rt.tracker.NextIndex,
            ScannedCount: rt.tracker.ScannedCount,
            Status:       rt.tracker.Status,
        }
    }
    
    return state.Save(resumePath, state.ResumeState{
        Chunks:   chunks,
        Generated: time.Now().Format(time.RFC3339),
    })
}
```

### Load on Resume

```go
func loadOrBuildChunks(inputs runInputs, resumePath string) ([]task.Chunk, error) {
    if resumePath == "" {
        return buildChunks(inputs)  // Fresh start
    }
    
    // Load existing state
    var resumeState state.ResumeState
    if err := state.Load(resumePath, &resumeState); err != nil {
        return nil, err
    }
    
    // Reconstruct chunks from resume state
    chunks := make([]task.Chunk, len(resumeState.Chunks))
    for i, cs := range resumeState.Chunks {
        chunks[i] = task.Chunk{
            CIDR:      cs.CIDR,
            // ... restore other fields
            NextIndex: cs.NextIndex,
            Status:    cs.Status,
        }
    }
    
    return chunks, nil
}
```

## 4. Resume Path Resolution

### CLI Flag

```bash
-resume /path/to/resume_state.json
```

### Path Resolution

| `-resume` flag | Save path | Load path |
|----------------|-----------|-----------|
| Set | Exact path provided | Exact path provided |
| Not set | `<output-dir>/resume_state.json` | Not loaded (fresh start) |

### Default Behavior

```go
func resolveResumePath(cfg config.Config) string {
    if cfg.Resume != "" {
        return cfg.Resume  // Explicit path
    }
    // Default: output directory
    return filepath.Join(cfg.Output, "resume_state.json")
}
```

## 5. Resume File Format

### Example

```json
{
  "chunks": [
    {
      "cidr": "10.0.0.0/24",
      "nextIndex": 100,
      "scannedCount": 100,
      "status": "scanning"
    },
    {
      "cidr": "10.1.0.0/24",
      "nextIndex": 0,
      "scannedCount": 0,
      "status": "pending"
    }
  ],
  "generated": "2024-03-18T12:34:56Z"
}
```

## 6. Design Decisions

| Decision | Rationale |
|----------|-----------|
| Temp-file + rename save | Never truncates the only copy of the resume state (§1) |
| Save on error only | Avoid unnecessary I/O |
| Don't save if complete | No point saving finished state |
| Manual resume flag | User must explicitly opt-in to resume |

## 7. Adding New Resume Fields

### Step 1: Add field to Chunk struct

```go
type Chunk struct {
    CIDR         string `json:"cidr"`
    NextIndex    int    `json:"nextIndex"`
    ScannedCount int    `json:"scannedCount"`
    Status       string `json:"status"`
    NewField     string `json:"newField"`  // Add
}
```

### Step 2: Update persistResumeState

```go
chunks[i] = state.Chunk{
    // ... existing fields
    NewField: rt.newField,
}
```

## 8. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/state/state.go` | JSON persistence |
| `pkg/state/signal.go` | SIGINT handling |
| `pkg/state/state_test.go` | Unit tests |
| `pkg/scanapp/resume_manager.go` | Resume orchestration |
| `pkg/scanapp/chunk_lifecycle.go` | Chunk loading |

## 9. Integration Points

- **CLI**: Resume path from `config.Config.Resume`
- **Scan**: `state.WithSIGINTCancel()` wraps context
- **Orchestration**: `persistResumeState()` called on cancel/error
- **Chunk**: Resume state loaded in `loadOrBuildChunks()`
