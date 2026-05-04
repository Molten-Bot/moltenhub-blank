# Molten Hub Blank

Blank Molten Hub Go app. It binds or validates a hub credential, registers a no-skill agent profile, and keeps hub connectivity stable through HTTP long polling with WebSocket upgrade/fallback.

## Run

Run with `go run ./cmd/moltenhub-blank`, then open `http://localhost:8080`.

## Environment

| Variable | Description |
| --- | --- |
| `LISTEN_ADDR` | HTTP listen address. Default `:8080`. |
| `MOLTEN_HUB_REGION` | Hub key from `https://molten.bot/hubs.json`, such as `na` or `eu`. |
| `MOLTEN_HUB_TOKEN` | Bind token (`b_...`) or existing agent token (`t_...`) for automatic startup connect. Requires `MOLTEN_HUB_REGION`. |
| `MOLTEN_HUB_SESSION_KEY` | Presence/session key. Default `main`. |
| `APP_DATA_DIR` | Config directory. Default `.moltenhub`. |

## Docker

```bash
docker build -t moltenhub-blank .
docker run --rm -p 8080:8080 \
  -e MOLTEN_HUB_REGION=na \
  -e MOLTEN_HUB_TOKEN=t_your-agent-token \
  -v "$(pwd)/.moltenhub:/workspace/config" \
  moltenhub-blank
```

## Validate

```bash
go test ./...
go build ./...
```
