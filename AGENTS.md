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

Two Go binaries. `cmd/server` is the dashboard: an Angular single-page app
plus a small JSON API in one process - no RPC layer, unlike `periscope2`'s
three-service split, because there is no capture pipeline or cross-machine
boundary here to justify one. `cmd/wifi-watcher` is a separate, optional
sidecar (see below) - it was kept out of `cmd/server` on purpose, since it
needs host Wi-Fi privileges the dashboard itself has no reason to have.

| Package | Job |
|---|---|
| `internal/bed` | HTTP+SSE client for the bed's own local API (no auth) |
| `internal/store` | SQLite event log; the only package that opens the DB |
| `internal/collector` | Holds the SSE connection, caches latest state, persists events, retargetable at runtime (`SwitchTarget`) |
| `internal/discovery` | Periodically sweeps the LAN for beds (`GET /api/device` on every candidate address) |
| `internal/analysis` | Pure, offline sleep-session summarization |
| `internal/web` | The only HTTP surface: JSON API + the embedded Angular build |
| `internal/wifiwatcher` | Orchestrates finding a bed still broadcasting its own AP and provisioning it onto the home network |
| `internal/wifi` | OS-specific `wifiwatcher.Radio` implementation (`netsh_windows.go` only so far - no Linux/`nmcli` one yet, see below) |

- `internal/web`'s `Live`/`History`/`Controller`/`Discoverer`/`Switcher`
  interfaces are declared in `web` (the consumer), implemented by
  `collector`, `store`, `bed.Client`, and `discovery.Scanner` respectively -
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
make build      # ng build, copy into internal/web/static/app, go build ./cmd/...
make run        # go run ./cmd/server, against BED_HOST (env, default 10.0.0.1)
make test       # go test ./... and the Angular unit tests
make fmt        # gofmt -w cmd internal
make vet        # go vet ./...
make lint       # vet + gofmt check + an Angular dev build
cd ui && npx ng serve                # UI dev server, proxies /api to localhost:8000 (proxy.conf.json)
BED_HOST=10.0.0.1 make deploy   # rebuild the image, kill and replace the running container (see compose.yaml for the Wi-Fi-AP note)
HOME_WIFI_SSID=... HOME_WIFI_PASSWORD=... make wifi-watcher   # Windows only; see cmd/wifi-watcher
```

`DB_PATH` (default `/data/restbed.db`) and `ADDR` (default `:8000`) are the
other two environment variables `cmd/server` reads.

Any `go` invocation (build, run, test, vet, fmt, ...) goes through a Makefile
target rather than being typed raw - add a target if the one you need isn't
here yet.
