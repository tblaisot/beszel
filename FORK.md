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
| F5 | CPU frequency chart | S | low | ⬆ |
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

**Change (as built).**
- **Parent host link.** Optional self-relation `parent` on `systems` (additive migration `1800000002_fork_system_parent.go`). A VM, LXC or container host points to the physical machine it runs on. This is a plain manual link: nothing is read from the hypervisor, so it works with Incus, Proxmox, libvirt or anything else. It is set with a **"Parent host" select in the system edit dialog** (saved immediately on change; not shown when adding a system, set it afterwards). Without a parent, a system is its own host.
- **Capacities.** Cores and total memory per system come from the existing `system_details` collection, loaded once with the `parent` links (two small queries, `requestKey: null` so the PocketBase SDK never auto-cancels them or the app's own calls) into a store (`src/fork/use-usage.ts`).
- **Mode toggle** (`Default` / `Absolute` / `% of host`, kept in local storage) next to the filter of the **All Systems** and **All Containers** tables:
  - *Default*: exactly what upstream shows.
  - *Absolute*: CPU as cores in use (`cpu% × system cores / 100`), memory in bytes. For systems the memory is `mp% × total`; containers already show absolute memory.
  - *% of host*: CPU cores in use / host cores, memory bytes / host memory, where the host is the parent system (or the system itself). In the systems table the meter bar follows the value shown.
  - Missing capacity falls back to the default cell. Columns stay sortable on the displayed value.
- **Two traps found while testing**, both caused by memoized rows and cached sort values: cells wrap the originals and read the mode from the store themselves (rebuilt column definitions never reach rows already rendered), and sorting uses a custom `sortingFn` plus a re-sort of the table when the mode or capacities change (TanStack caches accessor values and the sorted rows).

**Touchpoints (upstream files).** `systems-table.tsx` (hook, columns wrapper, toggle, re-sort: 4 small edits), `containers-table.tsx` (same), `add-system.tsx` (one field in the edit dialog). New: `src/fork/usage.ts` (+ test), `use-usage.ts`, `usage-columns.tsx`, migration and Go test.

**Done when.** With the toggle on "% of host", a small system and a big one are comparable in one sorted column, and "absolute" matches the system itself. Checked on a copy of the production DB: a 3 GiB guest with 779 MB used shows 779.1 MB (absolute) and 2.45 % (host, 32 GiB parent); the container table sorts by cores in use.

## F4. Top consumers chart

**Need.** Grafana's "RAM by container / CPU by stack, top 15 across systems" and Maintenant's *Top consumers* (CPU or memory, ranked bars over a window).

**Change (as built).**
- **Hub endpoint** `GET /api/beszel/fork/top-consumers?metric=cpu|memory&window=1h|12h|24h|1w|30d&limit=10` (auth required, limit 1 to 50, new file `internal/hub/fork_top_consumers.go`, one route line in `api.go`). It reads the `container_stats` tier that covers the window (`1m`, `10m`, `20m`, `120m`, `480m`: the retention tiers), only for the systems the user may read (members, or everyone with `SHARE_ALL_SYSTEMS`; superusers see all), averages each container over the records it appears in, and **ranks by absolute usage**: cores in use (`cpu% × system cores`, cores from `system_details`) for CPU, MiB for memory. This makes containers of differently sized systems comparable. The reply is about 1 KB.
- **UI:** a collapsible *Top consumers* card above the All Containers table: CPU / Memory buttons, a time window select, ten ranked bars with container, system, and value, refreshed every minute while the page is visible. Choices are kept in local storage. The value follows the F3 mode; the *Default* mode shows absolute values here (cores, bytes), because the ranking is absolute.
- **Not included:** a time-series overlay of all systems on one chart (Grafana style); noted as a possible later step.

**Touchpoints (upstream files).** `internal/hub/api.go` (one route line), `components/routes/containers.tsx` (import and one element). New: `internal/hub/fork_top_consumers.go` (+ test), `src/fork/top-consumers.tsx`.

**Measured on a copy of the production DB** (21 systems): the endpoint answers in under 8 ms for every window, including 30 days.

**Done when.** The card ranks containers across all systems, the order matches the absolute sort of the containers table, other users never see systems they cannot read (covered by a Go test), and records outside the window are ignored (covered by a Go test).

## F5. CPU frequency ⬆

**Need.** The old NAS dashboard had a CPU frequency chart; the hub stats have none.

**Host check (done on the NAS, intel i3-N305, `intel_pstate`).**
- **Fans: nothing to fix.** The hardware exposes no `fan*_input` to Linux (no Super-I/O driver finds a chip, no BMC; the ACPI "Fan" cooling devices are on/off states, not RPM) and the agent's sandbox does not hide anything. Upstream's `agent/fans.go` is correct and simply has nothing to read, so the fork does nothing for fans on this host. An optional chart of the ACPI fan cooling state is not worth adding (always 0 here).
- **CPU frequency is available** without privileges: per CPU `scaling_cur_freq` (kHz, varies with load) and `cpuinfo_avg_freq`; `cpuinfo_max_freq` for the ceiling. `cpuinfo_cur_freq` does not exist with `intel_pstate`, so it must not be required.

**Change (as built).**
- **Agent:** `agent/fork_cpu_freq_linux.go` reads `scaling_cur_freq` of every `cpuN/cpufreq` (falling back to `cpuinfo_avg_freq`; never `cpuinfo_cur_freq`) and the largest `cpuinfo_max_freq`, and reports `[average MHz, highest core MHz, ceiling MHz]` as the new optional stats key `cf` (cbor key 43). Hosts without cpufreq (most VMs and containers) omit it. Non-Linux builds get a no-op stub. One sysfs read per CPU per collection, no goroutine.
- **Hub:** longer records (10m and up) roll the key up with `internal/records/fork_cpu_freq.go`: average of the averages, peak of the peaks, latest ceiling; agents without the key do not drag the average down.
- **UI:** a *CPU Frequency* line chart (average and highest core, shown in MHz or GHz) on the system page, only when data exists (`src/fork/cpu-freq-chart.tsx`).
- Tested end to end with a real agent on a Linux dev machine (`[4261, 4426, 4800]` MHz) and a hub on a copy of the production DB; a system without cpufreq shows no chart.

**Touchpoints (upstream files).** `internal/entities/system/system.go` (one field), `agent/system.go` (one call), `internal/records/records.go` (three lines), `components/routes/system.tsx` (import and two elements, one per layout), `types.d.ts` (one field), `en.po`. New: the two agent files, `internal/records/fork_cpu_freq.go`, the chart, and tests with fake sysfs fixtures.

**Done when.** The NAS system page shows a CPU frequency chart that follows load; a host without cpufreq shows no chart and no error. (The NAS itself is covered by the host check above; the chart was verified with the dev machine's agent.)

## F6. "Needs attention" home view

**Need.** A view that answers "is everything OK?" at a glance, as Maintenant's dashboard does: empty when all is well, otherwise a ranked list of problems, plus a few numbers. It replaces the Homepage tiles that Beszel's widget cannot give (absolute CPU/RAM of the host, disk per pool, a list of problems).

**Change (as built).**
- **Hub endpoint** `GET /api/beszel/fork/attention` (auth required, new file `internal/hub/fork_attention.go`, one route line in `api.go`; the helpers `accessibleSystemIDs` and `inClause` moved to `fork_access.go`). It reads what the hub already stores, only for the systems the user may read, and returns `items`, `counts` and an `overview`. No new collection.
- **Problems listed**, critical first:
  - *critical*: a system that is down; a network monitor that is not responding (only monitors of systems that are up and have run at least once); a ZFS pool that is not `ONLINE` or `DEGRADED`; a S.M.A.R.T. device `FAILED`; a failed systemd service.
  - *warning*: a triggered alert (the `Status` alert is skipped, the down system already has its item); a `DEGRADED` pool; a S.M.A.R.T. `WARNING`; a container that is unhealthy, restarting, or exited with a non-zero code. Containers and units of a system that is down are ignored (stale rows).
  - *info*: a container image update available.
- **Overview numbers:** monitors up / total and uptime, average response of monitors that are up, systems up / total, updates available, **hosts** (systems that other systems name as their parent in F3, with CPU in cores used / total and memory used / total), and **storage pools** with used share and health.
- **UI:** a new route `/attention` and a bell icon with a red count of critical plus warning items in the navbar (desktop icon and mobile menu entry). The page has a banner (red, yellow, or green "Everything is OK"), the problem list (severity dot, name, type badge, reason, system, age, link to the matching page), the overview tiles, hosts, and pools. One request a minute while the page is visible, shared between the badge and the page.
- **Server route list:** `isAppRoute` in `internal/hub/server.go` must list `/attention`, or a direct load answers 404 (found while testing).
- **Not included (deliberately):** small curves of the host (CPU, memory, temperature, network); the system page has them and they would need a second data path. A setting to make this the landing page: it is an extra nav entry for now. A Homepage-embeddable view: the endpoint needs a user token, so a widget would need a token or a proxy.

**Touchpoints (upstream files).** `internal/hub/api.go` (one route line), `internal/hub/server.go` (one route in the list), `components/router.tsx` (one route), `main.tsx` (lazy import and one branch), `components/navbar.tsx` (import and two elements), `en.po`. New: `internal/hub/fork_attention.go`, `fork_access.go` (+ tests), `src/fork/attention-page.tsx`, `attention-nav.tsx`, `use-attention.ts`.

**Checked on a copy of the production DB** with seeded problems (a down system, failing monitors, a failed SMART device, a degraded pool, a failed unit, an unhealthy and a restarting container, image updates): 16 items in the right order and severities, badge count 14, mobile layout, and the green all-clear state. Go tests cover the rules (`buildAttention`), the endpoint's user scoping, and the route list.

## Write actions (F7 and F8)

Shared design, because these two break Beszel's read-only model and are the main security risk.

- **Off by default.** The agent only accepts an action if enabled by env, for example `FORK_ACTIONS=control,exec` (`control` = start/stop/restart of containers and services). Without it the handler returns "disabled" and the UI hides the buttons for that system (the agent reports its capabilities in the data it already sends).
- **Admins only** in the hub: enforced server-side on the hub endpoint (PocketBase role check), not only in the UI. Optionally limited per system by a flag on the system record.
- **Audit log.** Collection `fork_audit` (built with F7), written only by the hub and readable by admins; F8 adds its session start and end (never keystrokes).
- **Transport.** Reuse the existing hub↔agent channel (SSH or WebSocket) and add new `common.WebSocketAction` values at the **end** of the list in `internal/common/common-ws.go` so existing values keep their numbers; register new handlers from a `fork_` file with `registry.Register(...)`. Never relax the existing fingerprint or key checks.
- **Rebase risk.** `common-ws.go` and `agent/handlers.go` are shared with upstream: the diff there must stay at "append a constant, register a handler".

## F7. Container and service actions (start / stop / restart)

**Need.** Act on what the agent already lists, from the UI, without Portainer and without any hypervisor API. **The agent is the only bridge**: it talks to the Docker socket and to systemd on its own host, so the same code works on any machine (bare metal, VM, LXC, cloud). Nothing in the fork depends on Incus.

**Decisions (agreed before building).** The agent reports what it allows in its system info and the UI follows that (the hub checks again); admin role only; an audit collection plus a small admin list; build with fakes first and verify on a real Docker and systemd machine afterwards.

**Change (as built).**
- **Agent (`agent/fork_actions*.go`):** a new request `common.Control` (appended to the action list, one `registry.Register` line). It is refused unless the operator set `FORK_ACTIONS=control`; the enabled actions are reported in the system info (`fa`, cbor key 28) so the hub and UI know. Kinds:
  - *container*: `POST /containers/{id}/{start|stop|restart}` on the Docker socket, with a 60 s limit instead of the stats client's 2 s. Docker's `304` (already running or stopped) is reported as such. The id must be a 12 to 64 hex string.
  - *service*: D-Bus `StartUnit`, `StopUnit` or `RestartUnit` (mode `replace`), waiting for the job result. The name is what the hub stores (without `.service`) and is completed to `<name>.service`. It must match `SERVICE_PATTERNS` (the default `*.service` allows every service, so narrow it), must not be a timer, socket, target, mount or other unit type, and can never be the agent's own `beszel-agent*` unit. Linux only; other platforms answer "unsupported".
- **Hub:** `POST /api/beszel/fork/actions` with `{system, kind, target, action}` (`internal/hub/fork_actions.go`, one route line in `api.go`, admin middleware). It validates the input, requires the user to be able to read the system (404 otherwise), requires the agent to have reported `control` (403 otherwise), calls the agent, and writes one audit record for every attempt that gets past validation (`ok`, `error`, or `refused`). Errors from the agent come back as 502 with the message.
- **Audit:** collection `fork_audit` (user, email, system, kind, target, action, result, message, time), written only by the hub, listed only for admins, kept 180 days. The *Needs attention* page has a *Recent actions* card for admins.
- **UI:** Start / Stop / Restart buttons with a confirmation dialog and a toast, in the container detail sheet and the systemd service sheet. Shown only to admins and only for systems that report `control`.

**Agent permissions (documented, not automated).**
- *systemd:* an unprivileged agent is refused by polkit (`Access denied ... interactive authentication`, seen when testing as a normal user). Run the agent as root, or add a polkit rule for the agent's user and the units it may control.
- *Docker:* the agent needs access to the Docker socket and, with `control` on, can change any container there. A read-only bind mount of the socket does not make the Docker API read-only.

**Touchpoints (upstream files).** `internal/common/common-ws.go` (one constant and one request type), `internal/entities/system/system.go` (one info field), `agent/handlers.go` and `agent/agent.go` (one line each), `internal/hub/api.go` (one route line), `containers-table.tsx` and `systemd-table.tsx` (one import and one element each), `types.d.ts`, `en.po`. New: `agent/fork_actions.go`, `fork_actions_linux.go`, `fork_actions_other.go`, `internal/hub/fork_actions.go`, `fork_audit` migration, `internal/hub/systems/fork_control.go`, `src/fork/control-buttons.tsx`, `audit-list.tsx` (+ tests).

**Verified.** Go tests: agent (disabled by default, input validation, Docker calls against a fake Docker API including 204, 304, 404 and 500, service allow-list), hub (401 and 403 for non-admins, 404 for systems the admin cannot read, 400 for bad input, 403 when the agent does not report `control`, success and agent error paths, audit records, audit list visibility, nobody can write the audit log). End to end with a real agent and hub on a Linux dev machine over SSH: the capability reaches the hub and the buttons, the confirmation dialog, cancelling sends nothing, an action outside `SERVICE_PATTERNS` and the agent's own unit are refused, and an unprivileged D-Bus call reports the polkit error; every outcome is in the audit log.

**Not verified yet (needs a machine with Docker and systemd and a privileged agent).** A real container start, stop and restart (the dev machine has no Docker); a real service start, stop and restart (needs root or a polkit rule); the agent reconnecting after a restart of a unit it controls.

**Done when.** From the UI an admin can start, stop and restart a container and a listed service on any host; only the target changes; a non-admin, an agent without `FORK_ACTIONS`, or a unit outside `SERVICE_PATTERNS` is refused; each attempt writes one audit record. (The first clause is the part still waiting for the test VM.)

## F8. Container console (exec)

**Need.** Open a shell in a running container from the UI, like Portainer's console.

**Change (as built).**
- **Transport decision.** The console is carried by an **SSH session**, which is a natural bidirectional stream: the hub opens a session to the agent, sends one `Exec` request, the agent answers `ready`, and from then on both sides exchange `ExecFrame`s (data, resize, ping, exit; CBOR). **It works only when the hub reaches the agent over SSH** (the hub-dials-agent mode); agents that only connect to the hub through a WebSocket answer "the console needs the SSH connection". Supporting that mode would need a streaming extension of the WebSocket request manager, which is upstream code, so it is left out.
- **Agent (`agent/fork_exec.go`):** off unless `FORK_ACTIONS` contains `exec` (it is reported in the system info like `control`). It creates a Docker exec with a TTY running `bash` when the image has it and `sh` otherwise (`TERM=xterm-256color`), upgrades `POST /exec/{id}/start` to a raw stream, and pumps it both ways. Resizes go to `/exec/{id}/resize`; the exit code comes from `/exec/{id}/json`. Limits: 4 sessions per agent, 2 hours per session, closed after 15 minutes without input or output (pings do not count). Handlers receive the SSH session through a new optional `HandlerContext.Stream`.
- **Hub (`internal/hub/fork_exec.go`, `internal/hub/systems/fork_exec.go`):** two steps, because a browser cannot set headers on a WebSocket. An admin `POST`s `/api/beszel/fork/exec-ticket {system, container}`; this is where every check happens (admin role, system readable, agent reports `exec`, container id valid; a refusal is audited). The ticket is random, single-use, and valid for 30 seconds. The browser then opens `GET /api/beszel/fork/exec?ticket=...&cols=..&rows=..` as a WebSocket and the hub relays: binary messages are terminal bytes, text messages are JSON (`resize` from the browser; `exit` and `error` from the hub). The hub pings the agent every 25 s because the agent's SSH server drops connections idle for 70 s. At most 10 consoles are open on a hub. Audit records are written for the start and the end of every session (with its duration), and for failures; keystrokes and output are never recorded.
- **UI (`src/fork/console.tsx`, `console-dialog.tsx`):** a *Console* button next to Start, Stop and Restart in the container sheet, shown to admins for systems that report `exec`. It opens a dialog with an xterm.js terminal (`@xterm/xterm` and `@xterm/addon-fit`, loaded only when the button is used) that fits the dialog, follows window resizes, and shows the shell's exit code. Closing the dialog ends the shell.
- **Safety:** the console is remote code execution inside the container. It is off by default, admin-only, audited, and the ticket step means a page on another site cannot open one. Expose the hub's management audience only.

**Touchpoints (upstream files).** `internal/common/common-ws.go` (one constant, three types), `agent/handlers.go` (one field and one `Register` line), `agent/server.go` (3 lines to hand the SSH session to the handler), `internal/hub/api.go` (two route lines), `containers-table.tsx` (one import, one element), `package.json`, `bun.lock` and `package-lock.json` (two dependencies), `en.po`. New: `agent/fork_exec.go`, `internal/hub/fork_exec.go`, `internal/hub/systems/fork_exec.go`, `src/fork/console*.tsx` (+ tests).

**Verified.** Go tests: agent (the pump against a fake Docker API that really upgrades the connection: output, resize, ping, exit code, hub disconnect, idle timeout, refusals, session cap; run with the race detector), hub (ticket checks including the regular user, admin without access, agent without `exec` and the audit of the refusal; a real WebSocket client against the hub with a fake agent: echo, resize and its limits, exit message and close, single-use tickets, error path, started and ended audit records; race detector). End to end on a Linux dev machine with a real agent and hub over SSH and a small fake Docker daemon that bridges to a real pty: the terminal in the browser ran commands, the pty size matched the terminal (122x27), `exit 3` showed "The shell exited with code 3", the console survived 95 s of idling, closing the dialog ended the shell, and the audit log has the start and end.

**Not verified yet (needs a machine with real Docker):** a shell in a real container (the fake daemon follows the documented exec API but is not Docker), and an image without `sh`.

**Done when.** An admin can run a command, resize the window, and close the tab; the session ends on the agent within a few seconds of disconnect; a non-admin cannot open it. (All checked except against a real Docker daemon.)

---

## F9. Code-defined systems, monitors and alerts (last)

**Need.** "Everything is code": systems, network monitors and alerts are today set by hand in the UI or created by the `beszel-enrol` script through the API. Developed **last**, once the other features have settled the data model (monitor name, parent host).

**What upstream already does** (`internal/hub/config/config.go`): reads `config.yml` from the hub data dir; each entry of `systems:` has `name`, `host`, `port`, `token`, `users`; it creates and updates systems and **deletes systems that are no longer in the file**. It has no alerts, monitors or parent host.

**Change (as built).** The same `config.yml`, with everything nested **under the system it belongs to**:

```yaml
systems:
  - name: n8n
    host: 10.0.0.5
    users: [admin@example.com]
    parent: nas                 # F3, optional, by system name
    prune: false                # optional, see below
    alerts:                     # optional, one record per user of the system
      - { name: Disk, value: 80, min: 15 }
      - { name: Status }
    monitors:                   # optional, network monitors run by this system's agent
      - { name: Immich, protocol: http, target: "http://10.0.0.7:2283/", interval: 60 }
      - { name: DB, protocol: tcp, target: 10.0.0.8, port: 5432, disabled: true }
```

- Upstream's keys and behaviour are untouched; the new keys are optional, so an existing `config.yml` keeps working.
- **Reconcile rules.** Parent: set when it differs. Alerts: matched by `name` per user of the system, created or updated (`value`, `min`). Monitors: matched by `name` within the system, created or updated (`interval`, `disabled`, `skipTlsVerify`); if the probe itself changes (protocol, target, port, server) the monitor is **recreated**, because its id is a hash of those fields upstream. Nothing is deleted unless that system sets `prune: true`, which removes the alerts and monitors of that system that are not declared. Without it, things added in the UI survive.
- Alert names are checked against the `alerts` collection's allowed names (so new upstream alerts work without changes); an unknown name, a missing parent or an invalid entry is logged and skipped, never fatal.
- Monitors are created through the normal record path, so the upstream hooks run (stable id, sync to the agent when it connects). The default interval is 60 s.
- The sync runs at hub start only, as upstream's does.

**Touchpoints (upstream files).** `internal/hub/config/config.go`: one embedded struct in `systemConfig` and one call after the systems loop (5 lines). New: `fork_alerts_monitors.go` and its test.

**Done when.** Starting a clean hub with a `config.yml` produces the systems with their alerts and named monitors, a second start changes nothing, and a config without the new keys behaves exactly like upstream.

---

## Testing

Baseline (checked before the first fork change): all Go packages pass, 48 frontend tests pass, `golangci-lint` reports 66 issues that exist upstream (judge fork code by "no new issues").

- **Go:** `make test` (needs an `internal/site/dist/index.html`; `touch` an empty one, as `make dev-hub` does). One package: `go test -tags='testing no_ui' ./internal/hub/... -run TestX`.
- **Frontend:** in `internal/site`, `bun install` then `bun test` (uses `bunfig.toml`, which preloads a `localStorage` stub because the store modules read it at import time).
- **Lint and types:** `golangci-lint run`; in `internal/site` `bun run build` (Lingui + `tsc -b` + Vite).
- **Real data:** a copy of the production hub DB lives in `local/beszel-data/data.db`. `/local/` is excluded in `.git/info/exclude` and must never be committed (it holds the user record, password hash and hub settings). `local/fan-check/` holds the host-check notes. Work on a copy (`cp -r`), create a throwaway superuser with `beszel superuser upsert`, run `beszel serve --dir <copy> --http 127.0.0.1:8099`, and time the API calls with `curl`, to check F1 to F6 on real systems and monitors. F7 and F8 need a Linux VM with Docker, systemd and an agent with `FORK_ACTIONS` set.
- **F5 (CPU frequency):** the host check was done on the NAS (see F5); use fake sysfs fixtures for the unit tests.
- **Security checks (F7, F8):** non-admin refused, agent without `FORK_ACTIONS` refused, units outside `SERVICE_PATTERNS` refused, exec session closes on disconnect.

## Rebase and test checklist

- After each rebase: `make test` (`go test -tags='testing no_ui' ./...`), `golangci-lint run`, and in `internal/site` `bun run build` (includes `lingui extract/compile` and `tsc -b`).
- New user-visible strings use Lingui macros; only English source is edited, the rest comes from upstream's Crowdin flow.
- Each feature carries Go tests next to its `fork_*.go` file (use the existing `testing` build tag helpers) and a unit test for pure front-end helpers (`units.ts`, uptime computation).
- Keep a short `## Upstream touchpoints` list per feature current whenever an upstream file is edited.
- Check release notes of each upstream tag for overlap (upstream already shipped monitors, image update flag and shared rows-per-page; it may ship names, uptime or absolute values), and drop the fork's version when upstream's is equivalent.

## Open items to settle while implementing

- F9: upstream's config sync deletes systems missing from `config.yml`; decide whether the fork keeps that for systems (yes by default) while never deleting alerts/monitors.
