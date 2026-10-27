# FORK.md

Personal fork of [henrygd/beszel](https://github.com/henrygd/beszel). It adds the features missing for a multi-host homelab (many small Linux hosts or VMs, Docker containers and systemd services managed as code) while keeping Beszel light: the agent stays around 12 MiB and the hub around 12 MiB. Background and measurements are in `STUDY.adoc`; UI references are in `inspiration/` (Maintenant demo and the old Grafana dashboards).

## Ground rules

1. **Rebase-friendly.** `main` tracks upstream; work lives on a fork branch that is rebased regularly. No refactors, no renames, no reformatting of upstream files.
2. **New files first.** New code goes in new files with a `fork_` prefix (Go) or a `fork/` folder (React). Edits to upstream files are limited to one-line hook-ups (register a handler, add a route, add a column).
3. **One feature, one commit series**, so a feature can be dropped if upstream ships its own.
4. **Opt-in and light.** Anything that costs memory, CPU or opens a write path is off by default and enabled by an env var. No new dependencies unless unavoidable (frontend: reuse what is in `package.json`).
5. **Migrations are additive** (new collections or nullable fields, never altering upstream fields), with timestamps in the fork's own range so they never collide with upstream names.
6. **Touchpoints list.** Every upstream file a feature edits is listed under it below, and kept up to date, so rebase conflicts are predictable.
7. **Upstream candidates** (marked ⬆) are generic enough to offer as a PR to shrink the fork.

## Out of scope

Heartbeats (cron dead man's switch), security posture and CVE scanning, compose-stack grouping view, Prometheus `/metrics` export, log storage, compose editing or deploy or auto-update, own user management.

## Feature index and order

| # | Feature | Size | Risk | Upstream PR |
|---|---------|------|------|-------------|
| F1 | Name column on the monitors table | S | low | ⬆ |
| F2 | Uptime history column on the monitors table | S | low | ⬆ |
| F3 | Absolute / host-relative CPU and memory | M | low | ⬆ (toggle) |
| F4 | Top consumers chart | M | low | maybe |
| F5 | CPU frequency, fan check | S | low | ⬆ |
| F6 | "Needs attention" home view | M | low | no |
| F7 | Container and service start/stop/restart | M | high | no |
| F8 | Container console (exec) | L | high | no |
| F9 | Code-defined alerts and monitors in `config.yml` (last) | M | medium | no |

F1 to F5 are front-end heavy and nearly independent. Do them first. F9 is deliberately last. F7 and F8 share one authorisation and audit mechanism (see "Write actions").

---

## F1. Network monitor name column ⬆

**Need.** The Network Monitors table (the existing `/monitors` page) shows only the target (`192.168.101.2`, `192.168.102.20:5432`); the 37 registry monitors are hard to tell apart.

**Change.**
- Migration: add optional text field `name` to `network_monitors`.
- Table: a **new "Name" column** placed before "Target". **Target stays as it is** (with its status dot); Name shows the custom name, or an empty cell when none is set. Sortable, and included in the table filter.
- Monitor dialog (`monitor-dialog.tsx`): a Name input.
- Alert text for a monitor uses the name when set.
- Add `name` to `NETWORK_MONITOR_FIELDS` in `lib/use-network-monitors.ts`.
- `beszel-enrol` and F9 may set it.

**Touchpoints (upstream files).** `internal/site/src/components/network-monitors-table/{monitor-dialog,network-monitors-columns}.tsx` (one column entry, one input), `lib/use-network-monitors.ts` (one field name), `types.d.ts`, `internal/alerts/alerts_network_monitors.go` (message only). Agent is untouched.

**Done when.** A monitor created with a name shows it in its own column, the filter finds it by name, and monitors without a name look unchanged.

## F2. Uptime history column on the monitors table ⬆

**Need.** The table shows response time numbers only. For "is it up" the list should show an uptime history bar (a bar per period, green for up, red for down, grey for no data; as on Maintenant's endpoints page) so a failing monitor is obvious at a glance. This is a **new column in the existing Network Monitors table**, not a new page.

**Change.**
- New column **"Uptime"**, placed after Interval (before Response), with the bar and a `24h %` figure next to it. Sorting by the percentage puts the worst monitors first.
- **Data.** Already stored: each `network_monitor_stats` record carries `success_count` and `total_count`, so the availability of a period is `success_count / total_count` and no agent or schema change is needed. Segment colour: all successes = green, some failures = orange (partial), none = red, no record = grey.
- **Window and resolution (as built).** 24 h in 48 segments of 30 min, aligned to now. Source records: the `20m` tier (kept 24 h upstream, 72 records per monitor) plus the `1m` tier (kept 1 h). Where a segment has `1m` records they win over the `20m` ones, so nothing is double counted and the newest segments are exact; older segments use the `20m` counts. The constants live in `src/fork/monitor-uptime.ts`.
- **One query for the whole table**: a single `getFullList` on `network_monitor_stats` (fields `monitor,type,total_count,success_count,created`, filtered by the table's monitor ids), paged by PocketBase in batches of 1000. `20m` records cover the 24 h window; `1m` records are only fetched for the newest 30 min segment (they are fully covered there, and older ones would only add rows).
- **Measured on a copy of the production hub DB** (42 monitors, hub on localhost): all `20m` + all `1m` of the last hour = 6 207 rows, 7 requests, 586 KB, 0.20 s; with the 30 min `1m` window above = **3 771 rows, 4 requests, 359 KB, 0.11 s**, same percentages. Building the segments in the browser takes about 0.4 ms. If it is ever too slow over a remote link, the next step is one hub endpoint returning the compact per-monitor arrays (about 20 KB).
- **Refresh.** Re-fetch every 60 s while the page is visible (and when it becomes visible again) instead of subscribing to every stats record; the existing monitor subscription still drives the status dot.
- **Percentage.** `24h %` = sum(success) / sum(total) over the window, shown with one decimal and never rounded up to 100 % when there was a failure; `-` when no checks are recorded. Segment colours: green all successes, yellow some failures, red all failed, grey no data. Paused monitors are dimmed.
- Tooltip on a segment: time range, checks, failures, uptime %.
- Optional, later: the same bar and percentage in the per-monitor chart view.

**Touchpoints (upstream files).** `network-monitors-table.tsx` only (call the hook once, splice the column after Interval; two imports, three lines). Everything else is new under `src/fork/`: `monitor-uptime.ts` (+ test), `use-monitor-uptime.ts`, `uptime-column.tsx`.

**Done when.** A monitor that failed for 5 minutes shows a red or orange segment and a 24 h percentage below 100 %; a monitor with no data shows grey; the table makes one stats request per refresh regardless of the number of rows.

## F3. Absolute and host-relative CPU and memory ⬆ (toggle)

**Need.** All figures are a percentage of the machine they run in, so a small VM or LXC looks busy (66 % of 384 MiB) and systems cannot be compared. The goal is to find the biggest RAM and CPU users across systems with one unit and a sort.

**Change.**
- **Parent host link.** Optional field `parent` (relation to `systems`) on `systems` (additive migration). A VM, LXC or container host points to the physical machine it runs on. This is a plain manual link: nothing is read from the hypervisor, so it works with Incus, Proxmox, libvirt or anything else. Host totals (cores, RAM) come from the parent's `info`/stats; fall back to the system itself when unset.
- **Display modes**, a single toggle stored in local storage (like the shared rows-per-page setting): `% of machine` (today), `absolute` (MiB/GiB, cores or millicores), `% of host`.
- **Where it applies:** All Systems table (CPU, Memory columns), All Containers table (CPU, Memory), and the system page headline figures. Both columns are sortable on the displayed value.
- **Data:** system memory used is already in bytes (`mu`), container memory in MB (`m`); CPU absolute = `cpu% × cores / 100`. Container CPU is a % of its system, so absolute CPU = `cpu% × system cores / 100`. Needs the system's core count (already in system info).
- Settings UI for the system dialog: a "Parent host" select.

**Touchpoints.** `systems-table-columns.tsx`, `containers-table-columns.tsx`, `system/info-bar.tsx`, `add-system.tsx`/edit dialog, `types.d.ts`, one migration. Everything else in new `fork/units.ts`, `fork/use-display-mode.ts`.

**Done when.** With the toggle on "% of host", a small system and a big one are comparable in one sorted column, and "absolute" matches `free -m` on the system itself.

## F4. Top consumers chart

**Need.** Grafana's "RAM by container / CPU by stack, top 15 across systems" and Maintenant's *Top consumers* (CPU or memory, 1 h to 90 d ranked bars).

**Change.**
- A card on the Containers page (and reusable on F6's home view): ranked horizontal bars, switch CPU / Memory, window 1 h / 6 h / 24 h / 7 d / 30 d (limited by Beszel's retention: 1 m points for 1 h, then averages to 30 d), unit follows the F3 display mode.
- Aggregation: average over the window of each container's latest `container_stats` records. Prefer a **hub endpoint** (custom route registered from a `fork_` file) that returns the top N already aggregated, instead of the browser pulling every record of every system. Cost must stay a bounded query.
- Click a bar opens the container detail.

**Not included.** A time-series overlay of all systems on one chart (Grafana style); noted as a possible later step.

**Touchpoints.** `internal/hub/api.go` (route registration hook-up), `containers.tsx` (render the card). New `internal/hub/fork_top_consumers.go`, `fork/top-consumers.tsx`.

**Done when.** The card ranks containers across all systems in under a second with 17 systems, and the order matches the sorted column in the table for the 1 h window.

## F5. CPU frequency and fan check ⬆

**Need.** The old NAS dashboard had CPU frequency (Hz) and fan rpm. The study found neither in the hub stats.

**Change.**
- **Fans:** upstream already has `agent/fans.go` and a `Fans` field (`f`) in system stats. First verify on the NAS why it reports nothing (board exposes no fan, or sensor filtering). If it is a filter or config issue, document the env var; if it is a real gap, fix it in a new file.
- **CPU frequency:** add current (and optionally max) frequency per system from `/sys/devices/system/cpu/cpu*/cpufreq` (Linux only, stub elsewhere), as a new optional stats key and a small chart on the system page.
- Optional and cheap: one sysfs read per collection, no goroutine.

**Touchpoints.** `internal/entities/system/system.go` (one new field), `agent/system.go` or equivalent collector hook-up, `system.tsx` (chart), `chart-data.ts`. New `agent/fork_cpu_freq_linux.go` and `agent/fork_cpu_freq_other.go`.

**Done when.** The NAS system page shows a CPU frequency chart; fans appear when the hardware exposes them, with the reason documented when it does not.

## F6. "Needs attention" home view

**Need.** A dedicated home view, as in Maintenant's dashboard: empty when all is well, otherwise a ranked list of problems, plus a few numbers and curves. Replaces the Homepage tiles that Beszel's widget cannot give (absolute CPU/RAM of the NAS, disk per pool, list of problems, some curves).

**Problems to list** (all computed from data the hub already holds, no new collection):
- system down or alert triggered (status);
- network monitor failing (F1 name, F2 uptime bar);
- ZFS pool not `ONLINE`, SMART device not passing;
- systemd unit failed;
- container unhealthy, restarting, or exited unexpectedly;
- container image update available (existing upstream flag);
- active alerts from the alerts-history.

**Layout** (see `inspiration/` Dashboard screenshot).
- Top banner: "N need attention", counts by severity (incident / warning / OK).
- "Needs attention" list: severity dot, name, type badge (SYSTEM, MONITOR, ZFS, SMART, UNIT, CONTAINER, UPDATE), one-line reason, age, link to the relevant page.
- Overview tiles: global uptime (monitors up / total), average response time, NAS CPU and memory (F3 absolute), disk used per pool (`fast`, `tank1`, `tank2`, NVMe), updates (available / up to date).
- A few curves for the NAS (CPU, memory, temperature, network, last hours) reusing existing chart components.
- Entry point: new route and nav item, optionally set as the default landing page by a setting. The current home table stays as it is.

**Touchpoints.** `router.tsx`, `navbar.tsx` (one entry each). All logic in new `fork/attention/*` and optionally `internal/hub/fork_attention.go` if one aggregated endpoint is cheaper than several client queries. Must also work as a compact page that Homepage can embed (iframe) or read through one JSON endpoint (`/api/fork/attention`), so the widget need is met without a Homepage plugin.

**Done when.** With one monitor down and one container unhealthy the list shows exactly those two, sorted by severity, and it is empty (with the OK counters) otherwise.

## Write actions (F7 and F8)

Shared design, because these two break Beszel's read-only model and are the main security risk.

- **Off by default.** The agent only accepts an action if enabled by env, for example `FORK_ACTIONS=control,exec` (`control` = start/stop/restart of containers and services). Without it the handler returns "disabled" and the UI hides the buttons for that system (the agent reports its capabilities in the data it already sends).
- **Admins only** in the hub: enforced server-side on the hub endpoint (PocketBase role check), not only in the UI. Optionally limited per system by a flag on the system record.
- **Audit log.** New collection `fork_audit` (user, system, action, target, time, result; for exec: session start and end, no keystrokes). Append-only, admin-visible.
- **Transport.** Reuse the existing hub↔agent channel (SSH or WebSocket) and add new `common.WebSocketAction` values at the **end** of the list in `internal/common/common-ws.go` so existing values keep their numbers; register new handlers from a `fork_` file with `registry.Register(...)`. Never relax the existing fingerprint or key checks.
- **Rebase risk.** `common-ws.go` and `agent/handlers.go` are shared with upstream: the diff there must stay at "append a constant, register a handler".

## F7. Container and service actions (start / stop / restart)

**Need.** Act on what the agent already lists, from the UI, without Portainer and without any hypervisor API. **The agent is the only bridge**: it talks to the Docker socket and to systemd on its own host, so the same code works on any machine (bare metal, VM, LXC, cloud). Nothing in the fork depends on Incus.

**Change.**
- **Containers (Docker API):** `start`, `stop`, `restart` (`POST /containers/{id}/{action}`), added to the agent's docker manager. Allowed on any container the agent can see.
- **Services (systemd, D-Bus):** `start`, `stop`, `restart` through the dbus connection the systemd collector already opens (`StartUnit`, `StopUnit`, `RestartUnit`, mode `replace`). Restricted to the units the agent already lists, i.e. those matching `SERVICE_PATTERNS`; any other name is refused. A unit that wraps a Compose stack (for example `stack-<name>.service`, if you name them that way) is therefore a normal service, and restarting it reruns its pre-start steps; the fork has no "stack" concept of its own.
- **Service list and logs:** upstream already has the systemd table (`systemd-table`), `GetSystemdInfo` (details) and `GetSystemdLogs` (journal lines), and container logs/info. The fork only adds the action buttons next to them. First check that the existing views show status, sub-state and the last log lines as wanted; fill any gap there (for example a state filter or more log lines) in a new file rather than changing upstream components.
- **UI:** Start / Stop / Restart in the row menu and detail panel, with a confirmation dialog (Stop is the destructive one) and a result toast. Buttons are hidden when the agent reports the action as disabled. The audit record is written by the hub.
- **Agent permissions:** running as root, or a polkit rule, is needed for systemd writes; the Docker socket must be writable (not mounted read-only) for container actions. Documented, not automated.

**Touchpoints.** `internal/common/common-ws.go` (append constants), `agent/handlers.go` (register), container and systemd detail components (render the buttons). New `agent/fork_actions.go`, `internal/hub/fork_actions.go`, `fork/action-buttons.tsx`.

**Done when.** From the UI an admin can start, stop and restart a container and a listed service on any host; only the target changes; a non-admin, an agent without `FORK_ACTIONS`, or a unit outside `SERVICE_PATTERNS` is refused; each attempt writes one audit record.

## F8. Container console (exec)

**Need.** Open a shell in a running container from the UI, like Portainer's console.

**Change.**
- **Agent:** Docker exec create + attach with a TTY, bridged to a bidirectional stream (stdin, stdout, resize). One session per request, closed when the browser disconnects, with an idle timeout and a cap on concurrent sessions.
- **Hub:** a WebSocket endpoint (admin only) that opens the stream to the agent and relays frames. The current request/response pattern (`GetContainerLogs`) is one-shot, so this needs a **streaming action**; check the SSH transport can carry it as well as the WebSocket one before choosing, and support WebSocket first if SSH is hard.
- **UI:** a Console tab in the container detail using `xterm.js` (the one new frontend dependency; load it lazily so it does not weigh on the main bundle). Shell command choice: `/bin/sh` default, `bash` if present.
- **Safety:** never available unless `FORK_ACTIONS` contains `exec`; audit session start/end; documented as remote code execution in the container; recommended to expose the hub's management audience only.

**Touchpoints.** Same as F7, plus the hub route registration and `package.json` (xterm). New `agent/fork_exec.go`, `internal/hub/fork_exec.go`, `fork/console.tsx`.

**Done when.** An admin can run a command, resize the window, and close the tab; the session ends on the agent within a few seconds of disconnect; a non-admin cannot open it.

---

## F9. Code-defined systems, monitors and alerts (last)

**Need.** "Everything is code": systems, network monitors and alerts are today set by hand in the UI or created by the `beszel-enrol` script through the API. Developed **last**, once the other features have settled the data model (monitor name, parent host).

**What upstream already does** (`internal/hub/config/config.go`): reads `config.yml` from the hub data dir; each entry of `systems:` has `name`, `host`, `port`, `token`, `users`; it creates and updates systems and **deletes systems that are no longer in the file**. It has no alerts, monitors or parent host.

**Change.** Extend the same `config.yml`, with everything nested **under the system it belongs to** (the natural Beszel model: an alert and a network monitor each belong to one system):

```yaml
systems:
  - name: n8n
    host: 10.0.0.5
    users: [admin@example.com]
    parent: nas                 # F3, optional, by system name
    alerts:                     # optional
      - { name: Disk, value: 80, min: 15 }
      - { name: Status }
      - { name: SystemdFailed }
    monitors:                   # optional, network monitors run by this system's agent
      - { name: Immich, protocol: http, target: "http://10.0.0.7:2283/", interval: 60 }
```

- Keep upstream's top-level fields and behaviour untouched; the new keys are optional, so an existing `config.yml` keeps working. The fork's parsing lives in a new file; upstream's `systemConfig` struct gets the three new optional fields (the one unavoidable upstream edit).
- **Reconcile rules for the new keys:** create what is missing, update the declared fields, and **do not delete** alerts or monitors that are not declared (they may have been added in the UI), unless a `prune: true` is set on that system. Match alerts by `name`, monitors by `name` (F1).
- Alert names and fields must match the hub's alert records exactly (`internal/alerts`); unknown names are logged and skipped, not fatal.
- Secrets (hub key, admin password) stay in the environment, not in the file.
- Reuse existing record-creation paths so the usual hooks run (for example the monitor sync to the agent).

**Touchpoints.** `internal/hub/config/config.go` (add the optional fields to `systemConfig` and one call after systems are synced). New `internal/hub/config/fork_alerts_monitors.go`.

**Done when.** Starting a clean hub with a `config.yml` produces the systems with their alerts and named monitors, a second start changes nothing, and a config without the new keys behaves exactly like upstream.

---

## Testing

Baseline (checked before the first fork change): all Go packages pass, 48 frontend tests pass, `golangci-lint` reports 66 issues that exist upstream (judge fork code by "no new issues").

- **Go:** `make test` (needs an `internal/site/dist/index.html`; `touch` an empty one, as `make dev-hub` does). One package: `go test -tags='testing no_ui' ./internal/hub/... -run TestX`.
- **Frontend:** in `internal/site`, `bun install` then `bun test` (uses `bunfig.toml`, which preloads a `localStorage` stub because the store modules read it at import time).
- **Lint and types:** `golangci-lint run`; in `internal/site` `bun run build` (Lingui + `tsc -b` + Vite).
- **Real data:** copy a hub `data.db` into a dev hub (`make dev-hub`) to check F1 to F6 on real systems and monitors. F7 and F8 need a Linux VM with Docker, systemd and an agent with `FORK_ACTIONS` set.
- **F5 (fans, CPU frequency):** the host check is described in `FAN_TEST_NEEDS.md` and handled by a separate agent; its answer decides the F5 scope.
- **Security checks (F7, F8):** non-admin refused, agent without `FORK_ACTIONS` refused, units outside `SERVICE_PATTERNS` refused, exec session closes on disconnect.

## Rebase and test checklist

- After each rebase: `make test` (`go test -tags='testing no_ui' ./...`), `golangci-lint run`, and in `internal/site` `bun run build` (includes `lingui extract/compile` and `tsc -b`).
- New user-visible strings use Lingui macros; only English source is edited, the rest comes from upstream's Crowdin flow.
- Each feature carries Go tests next to its `fork_*.go` file (use the existing `testing` build tag helpers) and a unit test for pure front-end helpers (`units.ts`, uptime computation).
- Keep a short `## Upstream touchpoints` list per feature current whenever an upstream file is edited.
- Check release notes of each upstream tag for overlap (upstream already shipped monitors, image update flag and shared rows-per-page; it may ship names, uptime or absolute values), and drop the fork's version when upstream's is equivalent.

## Open items to settle while implementing

- F2: time the stats fetch on the real `data.db` (37 monitors) and tune the window / segment size if needed; retention was checked (`20m` kept 24 h).
- F5: why the NAS reports no fans (hardware, or filtered).
- F9: upstream's config sync deletes systems missing from `config.yml`; decide whether the fork keeps that for systems (yes by default) while never deleting alerts/monitors.
- F8: whether the SSH transport can carry a streaming action, or WebSocket-only agents get the console.
- Whether F6 becomes the default landing page, or stays an extra nav entry.
