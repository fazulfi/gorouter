# Quickstart

## Prerequisites

Install Go 1.25 and PostgreSQL. Keep credentials in environment variables; never commit secrets.

## Run locally

```bash
go run ./cmd/gorouter
```

Verify `GET /health` returns HTTP 200, then configure a provider using the [configuration guide](configuration.md).