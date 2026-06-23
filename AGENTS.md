# port-scan-mk3 Development Guidelines

## Build & Test Commands
- Build: `go build -o app ./cmd/app`
- Test all: `go test ./...`
- Test single: `go test -v -run <pattern> ./pkg/...`
- Lint: `golangci-lint run`
- Tidy: `go mod tidy`

## Coding Standards
- **Idiomatic Go**: Accept interfaces, return concrete types.
- **Error Handling**: Always check errors; use `fmt.Errorf` with `%w` for wrapping.
- **Naming**: Use camelCase for internal and PascalCase for exported symbols.
- **Concurrency**: Prefer channels for orchestration; avoid shared state where possible.
- **Modern Go**: Use built-ins from Go 1.21+ (e.g., `slices`, `maps`).
