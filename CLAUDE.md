# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Beszel is a lightweight server monitoring platform with two Go binaries plus a web UI:

- **Hub** (`internal/cmd/hub`, `internal/hub`): a [PocketBase](https://pocketbase.io/) app that serves the dashboard, stores data, runs alerts, and connects to agents.
- **Agent** (`agent/`, entrypoint `internal/cmd/agent`): runs on each monitored machine, collects metrics (CPU, memory, disk, network, Docker, GPU, sensors, S.M.A.R.T., ZFS, systemd, etc.) and returns them to the hub.
- **Web UI** (`internal/site`): React + TypeScript + Vite SPA, embedded into the hub binary via `internal/site/embed.go` (`//go:embed all:dist`).

Single Go module at the repo root (`github.com/henrygd/beszel`).

## Commands

Go (from repo root):

- `make test` — `go test -tags='testing no_ui' ./...` (the `testing` and `no_ui` tags are required)
- Single test: `go test -tags='testing no_ui' ./internal/alerts -run TestName`
- `make lint` — `golangci-lint run`
- `make build-agent` / `make build-hub` / `make build` — output to `./build`. Cross-compile with `OS=… ARCH=…`; `SKIP_WEB=true` skips the frontend build; `NVML=true|false|auto` controls the `glibc` build tag for NVIDIA NVML support.
- `make dev` (or `KEY="..." make -j dev`) — runs `dev-server` (Vite), `dev-hub` (hub on :8090 with `-tags development`), and `dev-agent`. Each can be run individually. `dev-hub` creates an empty `internal/site/dist/index.html` so the embed compiles.

Frontend (from `internal/site`; bun preferred, npm works):

- `bun run dev` — Vite dev server
- `bun run build` — `lingui extract` + `lingui compile` + `tsc -b` + `vite build`
- `bun run sync` — re-extract/compile i18n catalogs (`make generate-locales` does this if `src/locales/en/en.ts` is missing)
- Lint/format with Biome (`biome.json`); unit tests are `*.test.ts(x)` files under `src/lib` and `tests/`.

## Architecture

**Hub ↔ agent connection.** Agents are reached two ways: the hub opens an SSH connection to the agent (`internal/hub/transport/ssh.go`), or the agent connects to the hub over WebSocket (`internal/hub/ws`, `agent_connect.go`, `agent/client.go`). `internal/hub/transport` abstracts both behind one interface; shared wire code is in `internal/common`. The agent side is `agent/server.go` (SSH server) and `agent/client.go`/`connection_manager.go` (WebSocket client), dispatching requests via `agent/handlers.go`. Agents are authenticated by the hub's ed25519 key and a fingerprint (`agent/fingerprint.go`).

**Hub internals.** `internal/hub/hub.go` wires together subcomponents: `alerts.AlertManager` (`internal/alerts`), `users.UserManager`, `records.RecordManager` (averaging/rollup/deletion of stats records), `systems.SystemManager` (`internal/hub/systems`; polls each system and writes records), and `heartbeat`. Data structures shared between agent and hub live in `internal/entities/*`. Network monitors (ping/HTTP/cert probes) run in the agent (`agent/network_monitor*.go`) and are managed from the hub (`internal/hub/network_monitors.go`).

**Database.** PocketBase collections are defined by migrations in `internal/migrations` (initial snapshot plus timestamped Go migrations); `internal/hub/collections.go` handles collection setup/access rules. Schema changes need a new migration.

**Build tags / platform code.** Agent code is split by OS via filename suffixes and build tags (`*_linux.go`, `*_windows.go`, `*_unsupported.go`/`*_stub.go`, `gpu_nvml_*` with `glibc`). When adding platform-specific functionality, provide the stub for other platforms. Hub has `server_development.go` (`development`, proxies to Vite), `server_production.go` (`!development`, serves embedded dist), and `server_test.go` (`testing`).

**Frontend.** State is in nanostores (`src/lib/stores.ts`), routing via `@nanostores/router` (`components/router.tsx`), API/PocketBase access in `src/lib/api.ts` and `systemsManager.ts`. UI is shadcn/Radix (`components/ui`), charts in `components/charts`. Strings use Lingui macros; translations in `src/locales` are managed through Crowdin (`i18n.yml`), so only edit the English source and run `bun run sync`.

## Contributing notes

PRs follow `.github/pull_request_template.md` (description, docs link to the separate `beszel-docs` repo, changelog by Added/Changed/Fixed/Removed).
