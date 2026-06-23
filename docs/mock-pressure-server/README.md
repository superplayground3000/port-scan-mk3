# Mock Pressure API Server

本文件說明如何使用 mock pressure API server 進行本地測試。

## Overview

Mock pressure API server 支援兩種模式：
- **Simple Mode**: 無認證，直接回傳 pressure 值
- **Authenticated Mode**: OAuth-style client credentials 認證
- **Config-Driven Mode**: 從 JSON config 檔動態載入 `/api/pressure` response body（支援單一 object 或多 objects）

## Quick Start

```bash
cd e2e
docker compose up -d pressure-api-auth
```

Service 啟動後可透過 `http://localhost:8083` 存取。

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/pressure` | GET | Pressure endpoint（可回傳預設 `{ "pressure": n }` 或 config 指定 body） |
| `/api/pressure` | POST | 動態更新目前 pressure（`{"pressure":95}`）或 sequence（`{"sequence":[20,95,20]}`） |
| `/auth` | POST | OAuth token endpoint |
| `/data` | GET | Pressure data (requires Bearer token) |
| `/admin/config` | GET | 查看目前 config-driven response body（僅 config mode 啟用時可用） |
| `/admin/config/reload` | POST | 重載 `PRESSURE_RESPONSE_CONFIG` 指向的檔案（runtime 生效） |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ADDR` | `:8080` | Server listen address |
| `MODE` | `ok` | Simple mode: `ok`, `fail`, `timeout` |
| `PRESSURE` | `20` | Pressure value for simple mode |
| `PRESSURE_SEQUENCE` | empty | Comma-separated sequence, e.g. `20,95,95,20` |
| `PRESSURE_SEQUENCE_LOOP` | `false` | Whether to loop sequence when reaching the end |
| `DELAY_MS` | `5000` | Delay in ms for timeout mode |
| `PRESSURE_RESPONSE_CONFIG` | empty | Path to JSON file. When set, `/api/pressure` GET will return `response_body` from this file |
| `USE_AUTH` | `false` | Enable authenticated mode |
| `AUTH_CLIENT_ID` | `test-client` | Client ID for auth |
| `AUTH_CLIENT_SECRET` | `test-secret` | Client secret for auth |
| `PRESSURE_VALUE_1` | `85` | First pressure value (for data endpoint) |
| `PRESSURE_VALUE_2` | `72` | Second pressure value (for data endpoint) |

## Examples

### Simple Mode (No Auth)

```bash
# Start simple mode
docker run -d -p 8080:8080 \
  -e MODE=ok \
  -e PRESSURE=42 \
  e2e-pressure-api-auth

# Test
curl http://localhost:8080/api/pressure
# Output: {"pressure":42}
```

### Config-Driven Mode（單一/多 objects body）

範例 config 已提供在：

- `e2e/mock-pressure-api/configs/pressure-single-object.json`
- `e2e/mock-pressure-api/configs/pressure-two-objects.json`
- `e2e/mock-pressure-api/configs/pressure-three-objects.json`

```bash
# 啟動：以 2 objects config 作為 /api/pressure 回應 body
PRESSURE_RESPONSE_CONFIG=e2e/mock-pressure-api/configs/pressure-two-objects.json \
go run ./e2e/mock-pressure-api

curl -s http://localhost:8080/api/pressure
# Output: [{"data":{"Percent":95}},{"data":{"Percent":35}}]

# 修改 config 檔後，runtime 重載
curl -X POST http://localhost:8080/admin/config/reload

# 查看目前載入中的 config body
curl -s http://localhost:8080/admin/config
```

### Authenticated Mode

```bash
# Start auth mode
docker run -d -p 8083:8083 \
  -e ADDR=:8083 \
  -e USE_AUTH=true \
  -e AUTH_CLIENT_ID=test-client \
  -e AUTH_CLIENT_SECRET=test-secret \
  -e PRESSURE_VALUE_1=85 \
  -e PRESSURE_VALUE_2=72 \
  e2e-pressure-api-auth

# Step 1: Get access token
TOKEN=$(curl -s -X POST http://localhost:8083/auth \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "client_id=test-client&client_secret=test-secret" \
  | jq -r '.access_token')

# Step 2: Call data endpoint with token
curl -H "Authorization: Bearer $TOKEN" http://localhost:8083/data
# Output: [{"data":{"Percent":85}},{"data":{"Percent":72}}]
```

### Test Invalid Credentials

```bash
curl -s -w "\nHTTP Status: %{http_code}\n" \
  -X POST http://localhost:8083/auth \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "client_id=wrong&client_secret=secret"
# Output: unauthorized
# HTTP Status: 401
```

### Test Data Endpoint Without Token

```bash
curl -s -w "\nHTTP Status: %{http_code}\n" http://localhost:8083/data
# Output: unauthorized
# HTTP Status: 401
```

## Docker Compose Services

`e2e/docker-compose.yml` 提供以下預設服務：

| Service | Port | Description |
|---------|------|-------------|
| `pressure-api-ok` | random | Simple mode, returns pressure=20 |
| `pressure-api-5xx` | random | Returns 500 error |
| `pressure-api-timeout` | random | 5s delay then returns pressure=20 |
| `pressure-api-auth` | 8083 | Authenticated mode |

### Start All Pressure APIs

```bash
cd e2e
docker compose up -d pressure-api-ok pressure-api-5xx pressure-api-timeout pressure-api-auth
```

## Integration with port-scan CLI

使用 authenticated pressure API 進行掃描：

```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -pressure-use-auth \
  -pressure-auth-url=http://localhost:8083/auth \
  -pressure-data-url=http://localhost:8083/data \
  -pressure-client-id=test-client \
  -pressure-client-secret=test-secret
```

## Run Tests

```bash
cd e2e/mock-pressure-api
go test -v
```

## Build Image Manually

```bash
cd e2e/mock-pressure-api
docker build -t mock-pressure-api:test .
```
