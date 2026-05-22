# polar-library

Reverse-engineering knowledge base plugin for the [Polar](https://github.com/networkextension/Polar) platform.

Stores firmware blobs + per-function metadata for known devices. Used by polar-agent's MCP library adapter to look up firmware functions during RE sessions.

## Status

W2 handler migration combined with extraction at 2026-05-22. Library data is workspace-agnostic (global knowledge shared by all callers); cross-domain user lookups for "added_by" display go through dock SDK.

**Note**: `rev_agent_handlers.go` remains in Polar dock — agent-facing endpoints depend on dock's `PolarAgentAuthMiddleware`. Phase 3 will canonicalize.

## Install

```bash
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/library-svc ./cmd/library-svc
rsync -avz /tmp/library-svc local@<deploy-box>:/Users/local/.local/bin/
```

Environment:
- `POLAR_DOCK_URL`
- `POLAR_PLUGIN_TOKEN`
- `POLAR_LIBRARY_DB_DSN` (Postgres for `polar_library`)
- `POLAR_LIBRARY_LISTEN` (default `127.0.0.1:8088`)
- `POLAR_LIBRARY_BLOB_DIR` (firmware blob storage path)

## Endpoints

- `GET / POST / PUT / DELETE /api/rev/devices[/:id]`
- `GET / POST / PUT / DELETE /api/rev/firmwares[/:id]`
- `POST /api/rev/firmwares/:id/upload` — blob upload
- `GET /api/rev/firmwares/:id/blob` — blob download
- `GET / POST / PUT / DELETE /api/rev/functions[/:id]`

## Related

- [Polar dock](https://github.com/networkextension/Polar)
- [polar-sdk](https://github.com/networkextension/polar-sdk)

## License

MIT
