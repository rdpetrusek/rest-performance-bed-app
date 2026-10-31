# ReST Bed dashboard

A self-hosted replacement for the "ReST Bed" smart mattress's dead vendor cloud
(Vista Medical / Boditrak, both bankrupt - no vendor support path exists). This
is reverse-engineered, right-to-repair work on hardware the user already
bought: the official Android app, the bed's local HTTP+SSE API, and the
bundled firmware images are all documented from first principles in `docs/`.
The dashboard talks to the bed directly over the LAN, stores what it sees, and
serves live state, history, controls, and offline sleep-session analysis.
There is no cloud, no account, and no external AI call anywhere in this stack.

## Read the docs first

`docs/protocol.md` is the full REST/SSE API reference (endpoints, SSE events,
confirmed control flows, a "verified against a live bed" section with real
captures). `docs/hardware.md` identifies the three hardware generations from
the bundled firmware (the user's own bed is the middle one, Marvell 88MW300)
and has the firmware-disassembly repro steps. Look facts up there instead of
re-deriving them from the APK or firmware; when new testing confirms or
corrects something, update the matching doc.

## Standing constraints

- **No firmware pushes.** `PUT /api/firmware` (or any other OTA write) is
  never sent to a physical device unless the user explicitly says to push
  firmware in that session. Firmware work stays local analysis (disassembly,
  string/cross-reference work) until that explicit go-ahead.
- **Sleep analysis stays local and offline.** `internal/analysis` is pure
  arithmetic over the dashboard's own stored events - no network calls, no
  external LLM/AI API, now or when this grows new features. This was an
  explicit, deliberate choice (the user considered and declined a cloud/AI
  analysis layer), not an oversight to "fix" later.
- **Wi-Fi provisioning writes (`bed.Client.SetWifi`/`SetWifiMode`) are higher-
  stakes than the other control endpoints.** A wrong value can leave a bed
  needing the exact physical power-cycle-and-re-onboard dance this project
  exists to get away from. The sequence was confirmed live exactly once, as a
  supervised manual test, before `cmd/wifi-watcher` was built on top of it
  (2026-10-19) - treat any further change to that write sequence with the
  same care, not as routine as `/preferences/firmness` or `/air/*`.

## Architecture

Three services, each its own image:

| Service | Folder | Image |
|---|---|---|
| ui | `ui/` | Angular build on `nginx:1.27-alpine`; nginx also proxies `/api/` to the API (`API_UPSTREAM`, default `api:8000`) |
| api | `cmd/server/` | The Go JSON API on `distroless/static` - no UI embedded, no RPC layer (no capture pipeline or cross-machine boundary to justify one) |
| wifi-watcher | `cmd/wifi-watcher/` | Not containerized yet - needs the Linux `nmcli` Radio first (see below) |

`cmd/wifi-watcher` is a separate, optional sidecar - kept out of
`cmd/server` on purpose, since it needs host Wi-Fi privileges the API has no
reason to have.

**Everything specific to a service lives in that service's folder**: its
`Dockerfile` (plus `Dockerfile.dockerignore` / `.dockerignore`), its
`compose.yaml` fragment, and its config (e.g. `ui/nginx.conf.template`). The
root `compose.yaml` only names the project and `include:`s each fragment;
the root `Makefile` only delegates UI work to `ui/package.json` scripts.
Removing a service means deleting its folder and its `include:` line. The
root `Dockerfile`, `Dockerfile.fw` and `Dockerfile.ghidra` are
reverse-engineering tooling images, not services.

| Package | Job |
|---|---|
| `internal/bed` | HTTP+SSE client for the bed's own local API (no auth) |
| `internal/store` | SQLite event log; the only package that opens the DB |
| `internal/collector` | Holds the SSE connection, caches latest state, persists events, retargetable at runtime (`SwitchTarget`); idle while there is no target |
| `internal/connection` | Which bed is picked (by id, saved in the store) and where it lives now: verifies the address, follows the bed when it moves, and runs commands with an identity check and one rescan-and-retry |
| `internal/discovery` | Periodically sweeps the LAN for beds (`GET /api/device` on every candidate address); sweeps never overlap and concurrent callers share one |
| `internal/analysis` | Pure, offline sleep-session summarization |
| `internal/web` | The JSON API (`/api/*` only; the UI is served by nginx in `ui/`) |
| `internal/wifiwatcher` | Orchestrates finding a bed still broadcasting its own AP and provisioning it onto the home network |
| `internal/wifi` | OS-specific `wifiwatcher.Radio` implementation (`netsh_windows.go` only so far - no Linux/`nmcli` one yet, see below) |

- `internal/web`'s `Live`/`History`/`Controller`/`Discoverer`/`Switcher`
  interfaces are declared in `web` (the consumer), implemented by
  `collector`, `store`, `connection.Manager` (which sends commands through
  `bed.Client` after checking the bed's id, and handles picks), and
  `discovery.Scanner` respectively -
  the one real seam this app needs, there for testability (fakes in
  `web_test.go`), not for swappability that will never happen.
  `internal/wifiwatcher`'s `Radio`/`Provisioner` interfaces follow the same
  pattern against `internal/wifi` and `bed.Client`.
- `cmd/server/main.go` and `cmd/wifi-watcher/main.go` do all the wiring;
  nothing elsewhere holds package-level mutable state.
- `ui/`: the Angular workspace. `ui/.claude/CLAUDE.md` has its own rules and
  loads automatically when working under `ui/`.

### `cmd/wifi-watcher`

A bed only ever broadcasts its own direct-mode AP (`ReST-<id>`) when it has
*not* successfully joined a home network - the two are mutually exclusive on
this hardware (see `docs/hardware.md`). `wifi-watcher` periodically scans for
that SSID pattern, and for each one found: joins it, confirms the bed is
reachable (`GET /device`), provisions it with `HOME_WIFI_SSID`/
`HOME_WIFI_PASSWORD` (env), switches it to indirect mode, then leaves and
rejoins whatever network this host was on before - Ethernet stays up the
whole time, so the dashboard is never affected by this process joining and
leaving Wi-Fi networks. Once a bed is actually indirect, `internal/discovery`
(already running in `cmd/server`, already sweeping every subnet this host has
an interface on) finds it on the LAN like any other device - no coordination
between the two binaries is needed beyond that.

Currently Windows-only (`internal/wifi/netsh_windows.go`, built on `netsh
wlan`). The real deployment target is a Linux Pi, which needs an `nmcli`-based
implementation of `wifiwatcher.Radio` - not written yet. Plan when that's
picked up: run it as a plain host-level process (not inside the dashboard's
container), since it needs `network_mode: host` plus NetworkManager/D-Bus
access that the minimal `distroless` dashboard image has no reason to carry.

## Conventions

- Go: the Google Go Style Guide. Interfaces are declared by the consumer, only
  at real seams; constructors return concrete types; no package-level mutable
  state; wiring happens only in `cmd/server/main.go`; `Run(ctx) error` for
  anything long-running.
- Angular: the official style guide and `ui/.claude/CLAUDE.md` (standalone,
  signals, `inject()`, OnPush, native control flow). One feature,
  `ui/src/app/features/bed/`, since the whole dashboard is one page about one
  bed - `bed-api.ts` wraps the HTTP calls, `bed-store.ts` holds the signals
  every component reads.
- Tests: table-driven, plain `testing` + `cmp.Diff` (no assertion libraries);
  `internal/store`'s tests use a real temporary SQLite file, never a mock.
  Firmware/hardware reverse-engineering work stays test-light: when mapping
  something new from the APK or firmware, decode it, show the user, and ask
  before writing code - that work belongs in `docs/`, not in test fixtures.

## Commands

```bash
make build      # go build ./cmd/... (API + wifi-watcher binaries)
make ui         # cd ui && npm run build (the Angular app; served by nginx in its image)
make run        # go run ./cmd/server (API only); finds the bed by scanning, never by a configured address
make test       # go test ./... and the Angular unit tests
make fmt        # gofmt -w cmd internal
make vet        # go vet ./...
make lint       # vet + gofmt check + an Angular dev build (ui: npm run lint)
cd ui && npm start                 # UI dev server on :4200, proxies /api to localhost:8000 (proxy.conf.json)
make deploy       # rebuild the ui + api images and replace their containers; dashboard on :8097 (see compose.yaml for the Wi-Fi-AP note)
HOME_WIFI_SSID=... HOME_WIFI_PASSWORD=... make wifi-watcher   # Windows only; see cmd/wifi-watcher
```

`SCAN_SUBNETS` (comma-separated /24 prefixes such as `192.168.1`) names LANs to
sweep that this process has no interface on (a container on Docker Desktop or a
cluster pod); the host's own interfaces are always swept. `DB_PATH` (default
`/data/restbed.db`) and `ADDR` (default `:8000`) are the other environment
variables `cmd/server` reads. There is no bed address setting: the bed is
found by scanning on every start, and the user's pick is stored as the bed's id.

Any `go` invocation (build, run, test, vet, fmt, ...) goes through a Makefile
target rather than being typed raw - add a target if the one you need isn't
here yet.
