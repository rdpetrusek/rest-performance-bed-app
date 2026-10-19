# ReST Bed app (com.restperformance.rest 2.11) - protocol notes

Source: decompiled APK, `out/apktool/assets/public/assets/index-b3d7c305.js` (beautified copy line numbers
below refer to `prettier --parser babel` output). The app is a Capacitor/React web app; Java code is only a shell.
Everything here is read from client code. Nothing was tested against a real bed, so request/response shapes marked
(?) are inferred.

## Architecture
- Phone talks directly to the bed over plain **HTTP on the LAN, port 80, no authentication, no TLS** (`cleartext: true`).
- Bed = ESP/Linux "pump" controller serving a REST API under `/api`, plus an SSE stream for live state.
- Cloud (Azure Functions + Auth0) is only used for optional sleep-event history and registration, not for control.

## Discovery (lines ~63000-63110)
1. **mDNS**: browse `_http._tcp.` in `local.`; accept services whose name contains `ReST-` or `SM-` with an IPv4 address.
   Then `GET /api/device` to confirm.
2. **Deep scan** (fallback/web): learn own IP via WebRTC ICE candidate, then `GET http://<a.b.c.N>/api/device`
   for N=1..254 (5 s timeout; also seeds with 10.0.0.x). Accept if JSON `class` is `"ReST Bed"` or `"SmartBed"`.
- Device ID shown as `ReST-<id>` (AI Dream brand uses bare `<id>`).

## Device model
`GET /api/device` returns JSON including `id`, `name`, `address`, `class`, `model`, `configuration`.
`model` code -> hardware / firmware file / latest firmware version (lines ~20375-20440):

| model | hw | firmware file | version |
|---|---|---|---|
| daa / undefined | P1 | p1-stable.deb | 0.10.098 |
| maa | P2 | p3-stable.bin | 0.08.003 |
| mab | P3 | p3-stable.bin | 0.08.003 |
| mac | P3A | p3-stable.bin | 0.08.003 |
| eaa | P3B | p3b-stable.bin | 1.08.040 |
| eab | P3C | p3b-stable.bin | 1.08.040 |
| eac, ead | P3D | p3b-stable.bin | 1.08.040 |
| exa | P6 | p6-esp1.bin (not bundled) | 1.00.063 |

`configuration` is a string such as `pubb4z00ha` (suffix `00ha` is tested for a feature); `"3z"` substring = 3-zone bed.

## REST endpoints (all `http://<bed>/api/...`)
Bodies are JSON literals sent with `content-type` unset (text/plain) via PUT. Success is 200/204.

| method | path | body / response |
|---|---|---|
| GET | `/device` | device info JSON (2 s timeout during scan) |
| PUT | `/device/name` | `"<string>"` |
| PUT | `/device/configuration` | `"<string>"` |
| GET | `/sse` | Server-Sent Events, see below |
| GET/PUT | `/firmware/version` / `/firmware` | PUT raw firmware binary (`text/plain`) = OTA update |
| GET | `/wifi/list` | scan results (called while bed is in AP mode) |
| GET | `/wifi` | current wifi config |
| PUT | `/wifi` | `{"ssid":"..","password":".."}` -> 204 |
| DELETE | `/wifi` | forget network |
| PUT | `/wifi/mode` | `"direct"` or `"indirect"` (JSON string; "indirect" = join home network, sent 500 ms after PUT /wifi) -> 204 |
| PUT | `/preferences/mode` | `"pressure"` (automatic) / `"manual"` / `"position"` |
| PUT | `/preferences/firmness` | number (raw) |
| PUT | `/preferences/distortion` | number (raw) |
| PUT | `/preferences/manualprofile` | `[a,b,c,d]` four zone pressures (numbers) |
| PUT | `/preferences/<key>` | `[a,b,c,d]`; key = bed position id (position mode) |
| GET/PUT | `/values/<key>` | array of 4 numbers; key `manual`/`profile1`/`profile2` = saved profiles (app reads then writes to copy) |
| GET | `/values` | 200 = reachable check |
| PUT | `/values/present` | `true` (calibration step: bed occupied?) |
| PUT | `/body/present` | `true`/`false` (calibration) |
| PUT | `/air/flows` | `["off"|..., x4]` per-zone pump flow command (all "off" = stop) |
| PUT | `/air/filling` | `true` = start fill |
| PUT | `/cloud/id` | `"<azure IoT connection string>"` |
| PUT | `/cloud/enabled` | `true` |
| GET/PUT | `/storage/writable` | bool; makes bed filesystem writable (for updates?) |

## SSE `/api/sse` events
Each event `data:` is JSON; event names: `device`, `surface`, `preferences`, `air`.
- `air` has `pressures[]` and `flows[]` (4 zones; 3-zone beds drop element 0 for display).
- `preferences` has `manualprofile`, `firmness`, `distortion`, mode.
- `surface` = pressure-map grid (`rows`, `columns`, `pressures`).
- App closes the stream when backgrounded and falls back after 5 s if not OPEN.

## Typical control flows
- **Set manual zone firmness**: `PUT /preferences/mode "manual"`, `PUT /preferences/manualprofile [p1,p2,p3,p4]`, then `PUT /air/filling true`.
- **Stop**: `PUT /air/flows ["off","off","off","off"]`, `PUT /preferences/<position> <current pressures>`.
- **Load saved profile**: `GET /values/profile1` -> `PUT /preferences/manualprofile <same>`.
- **Onboard**: join bed's AP (`ReST-<id>`), `GET /wifi/list`, `PUT /wifi {ssid,password}`, `PUT /wifi/mode "indirect"`.
- **OTA**: `PUT /firmware` with the .bin/.deb body; poll `/firmware/version`.

## Cloud
- Auth0: domain `rest-bed.auth0.com`, client `cXgdUxapFrRdg2W3Z1tKWd7rxvoPL8aq`, callback `restperformance://rest-bed.auth0.com/cordova/restperformance/callback`.
- `POST https://rest-bed-registration.azurewebsites.net/api/register` (user registration JSON: email, firstName, lastName).
- `POST https://rest-sleep-events-register.azurewebsites.net/api/Register` `{pumpId}` + Bearer -> `{connectionString}` which is pushed to the bed via `/cloud/id`.
- `GET https://rest-sleep-events-retrieve.azurewebsites.net/api/Retrieve?pumpId=&start=&end=` + Bearer.

## Bundled firmware (APK `assets/public/firmware/`)
`p1-stable.deb`, `p3-stable.bin`, `p3b-stable.bin`. Worth a `binwalk`/`strings` pass for the device-side API.

## Open questions / next
- Exact JSON schema of `/api/device` and SSE events (needs a live capture or firmware strings).
- Meaning of `<key>` values for position preferences (find `Position` enum near line ~35800).
- Whether the bed API is reachable unauthenticated from any LAN host (client sends no credentials).

## Verified against a live bed (2026-10-18)
Bed `ReST-ee0d07`, model `mac` (P3A), config `publ5z65ha`, firmware `0.08.001` (latest bundled: 0.08.003).
Raw captures in `docs/captures/`.

- **Setup AP**: open (no password) SSID `ReST-<id>`, 2.4 GHz; bed is `10.0.0.1`, DHCP gives clients `10.0.0.x`. `wifi.mode` = `"direct"`.
- **No authentication**: all GETs answered 200 to an anonymous client. Only read-only requests were sent in this session.
- Responses are JSON with `application/json`. Only GET endpoints were exercised.

Confirmed shapes:
```
GET /api/device       {"class":"ReST Bed","name":"ReST-ee0d07","id":"d8c0a6ee0d07","address":"10.0.0.1","model":"mac","configuration":"publ5z65ha"}
GET /api/wifi         {"ssid":"","mode":"direct","list":["<ssid>",...]}
GET /api/wifi/list    ["<ssid>",...]
GET /api/firmware/version  "0.08.001"
GET /api/values       {"profile1":[34,33,23,25],"manual":[26,16,18,26]}
GET /api/storage/writable  true
```
`id` is the MAC address without colons; `name` is `ReST-` + last 6 hex digits.

### SSE `/api/sse` (corrections to earlier notes)
Events sent on connect, then `time` every 1 s:
```
event: device       {class,name,id,address,model,configuration}
event: preferences  {"mode":"manual","firmness":28,"distortion":2,"quiet":false,"tolerance":2,"sensitivity":80,
                     "backprofile":[4],"sideprofile":[4],"manualprofile":[4]}
event: body         {"present":true,"position":"unknown","moving":false,"respiration":0.0,"heartrate":0,
                     "breathing":{"rate":0.0,"depth":0.0}}
event: air          {"filling":false,"targets":[4],"pressures":[4],"flows":["off"x4],"minimum":5,"maximum":50}
event: surface      {"columns":0,"rows":0,"width":0,"height":0,"units":"mmHg","pressures":[]}
event: temperature  {"cpu":50.1,"enclosure":23.9,"cpumax":80,"enclosuremax":50,"overheated":false}
event: time         "YYYY-MM-DD HH:MM:SS"
```
So there is also a `body` event (heart rate / respiration / position) and a `temperature` event that the app code
does not wire up as listeners in the main view. `preferences` has `backprofile`/`sideprofile`/`manualprofile`
(four-zone pressure targets, units appear to be mmHg-like integers 5..50) which explains `/preferences/<key>`:
the key is presumably `back`/`side`/... profile names (unverified, needs a PUT test).

### Still unverified (PUT/DELETE never sent)
All write endpoints, valid `flows` strings other than `"off"`, `/preferences/<position>` key names.

## Firmware analysis (2026-10-18)

Extracted and analyzed the three images bundled in the app (`assets/public/firmware/`) inside the
`restbed-fw` Docker container (Ubuntu 24.04 + file/binwalk/binutils/squashfs-tools/dpkg). See `firmware/`
(gitignored; images not committed - large binaries, not needed since routes are documented here) and
`Dockerfile.fw` / rebuild with:
```
docker build -f Dockerfile.fw -t restbed-fw .
docker run --rm -v "<repo>/firmware:/data" restbed-fw -c '<cmd>'
```

### p1-stable.deb — P1 pump box (armhf Debian package)
- `smartbed 0.10.098-1`, arch armhf, maintainer `Grant Derksen <grant@vista-medical.com>` (OEM: **Vista Medical**).
- Depends on `bluez`, `libusb`, `libiw30`, `sqlite3`, `python-requests` — an old (Debian 7/wheezy-era) embedded Linux image.
- Main binary `/usr/sbin/smartbed` — stripped ARM ELF, implements the HTTP API + webserver (serves `/var/www`,
  a jQuery/Bootstrap debug UI at `index.html`/`debug.html`).
- `/opt/smartbed/vitals` — separate unstripped ELF, presumably reads the pressure-sensor "vitals" (heartrate/respiration).
- `/var/cache/smartbed/libnl.so.1.1` — netlink helper (Wi-Fi).
- Companion scripts: `getwifilist` (`iw wlan0 scan`), `setwifimode` (direct=hostapd AP / indirect=client via
  `/etc/network/interfaces.{direct,indirect}`, random channel 1-11 each time AP mode starts), `btdiscovery`
  (opens classic-BT discoverable for 10s via `hciconfig`, present but disabled in `rc.local`), `sbmonitor`
  (watchdog: curls `127.0.0.1/api/device` every 10s, kills+restarts `smartbed` on failure), `sbuploader.py`
  (cron **every minute**, uploads un-sent rows from local SQLite `events` table to
  `http://www.vista-medical.com/rest/api/v1/events` over **plain HTTP**, POST form body `pump,time,type,version,state`).
- Hostname/AP SSID `ReST-<id>` is derived in `rc.local` from `wlan0`'s MAC (bytes 4-6, hex, no colons) at every boot.
- `smartbed` binary strings confirm: no auth tokens/API keys anywhere; the only "password" string is the
  Wi-Fi join payload field. SQL: single `events` table (`sent, time, type, version, state`). Uses
  `/dev/spidev0.0` and `/dev/spidev0.1` to talk to the air-bladder valve hardware — log strings `Opened/Closed/
  Locked/Unlocked valve %d`, `Start/Completed valve cycling`.
- **Full route table** pulled from strings (superset of what the app's JS calls — confirms + extends earlier list):
  `/api/air[/cal|/filling|/flows|/gauges|/maximum|/minimum|/pressures|/sse|/targets]`,
  `/api/body[/breathing[/depth|/rate]|/heartrate|/moving|/position|/present|/respiration]`,
  `/api/cloud[/connected|/enabled|/id]`,
  `/api/device[/address|/class|/configuration|/id|/model|/name]`,
  `/api/events`, `/api/firmware[/version]`, `/api/log`,
  `/api/preferences[/backprofile|/cyclevalves|/distortion|/firmness|/manualprofile|/mode|/quiet|/sensitivity|/sideprofile|/tolerance]`,
  `/api/sse`, `/api/storage[/capacity|/used|/writable]`,
  `/api/surface[/columns|/height|/pressures|/rows|/units|/width]`,
  `/api/temperature[/checkoverrun|/cpu|/cpumax|/enclosure|/enclosuremax|/overheated]`,
  `/api/time`, `/api/upgrade`, `/api/uptime`, `/api/values[/<key>]`, `/api/version`,
  `/api/wifi[/list|/mode|/provisioned|/ssid]`, `/api/www[/version]`.
  (Not all of these were seen called from the current app JS bundle - some look like legacy/debug-UI-only routes,
  e.g. `/api/version`, `/api/www/version`, `/api/log`, `/api/upgrade` vs the app's `/api/firmware`.)
- References Azure cloud auth via string `azure_iot_client` and `/?api-version=2018-06-30` (Azure IoT Hub/DPS REST
  API version), matching the app's `cloud/id` = IoT Hub device connection string flow documented above.

### p3-stable.bin — P2/P3/P3A (ESP32, Espressif IDF, not recognized by `file`'s magic but same family as below)
- Same route surface as p1 (confirms it's a reimplementation of the same API on ESP32 instead of the Linux
  pump box) plus a `vista-medical`-style cert blob, AES tables, CRC32 tables typical of ESP-IDF TLS/OTA.
- String `azure_iot_client` also present — same Azure IoT Hub cloud path.

### p3b-stable.bin — P3B/P3C/P3D (confirmed ESP32-S2, ESP-IDF)
- `file`: ESP-IDF application image, project `rest_espressif`, firmware version string
  `1.08.039-1-ga5a8c0b-dirty`, built 2023-04-25, IDF `v4.4-dev-2184-g166c30e7b2`.
- Uses `esp_mqtt_client_init` / ESP-MQTT with string `azure-devices` → confirms cloud transport moved from
  Azure IoT Hub's HTTPS/AMQP (P1/P3) to **MQTT over TLS to `*.azure-devices.net`** on the newer boards.
- Embedded PEM certificate + RSA/EC private key blobs (binwalk hits) — likely the device's per-unit X.509
  identity for IoT Hub mutual-TLS auth; **did not extract/decode these from the shared firmware image** (would
  need the actual per-device cert from a specific unit's flash, not the generic update package — the one in
  the app bundle is probably a template/stub, not a live secret, but didn't verify).
- Route prefixes visible: `air/, body/, cloud/, device/, firmware/, log, monitor/, preferences/, speedtest,
  sse, storage/, surface/, temperature/, time, uptime, values/, wifi/` — consistent with p1/p3 surface, adds
  `/api/monitor` and `/api/speedtest`.

### Take-aways for the protocol doc above
- The `/preferences/<key>` key names are now confirmed as a flat namespace alongside `mode`/`firmness`/etc:
  `backprofile`, `sideprofile`, `manualprofile`, `distortion`, `sensitivity`, `tolerance`, `quiet`, `cyclevalves`
  (not `manual`/`profile1`/`profile2` as originally guessed for that specific route — those are `/api/values/<key>`
  save-slots instead).
- `/api/air/cal`, `/api/air/gauges`, `/api/air/maximum`, `/api/air/minimum` are additional undocumented-from-JS
  air endpoints.
- Confirms zero authentication at the firmware level too — the binary has no token/secret check strings, it's
  architecturally open-by-design on the local AP/LAN; the only "security" is the closed Wi-Fi network boundary
  (and on this unit, that network boundary is itself open/unencrypted).
- Cloud telemetry upload (`sbuploader.py`) goes out over **plaintext HTTP** to `www.vista-medical.com`, separate
  from the HTTPS Azure Functions endpoints the app itself calls.

## Control endpoints verified against live hardware (2026-10-19)

Tested through the new app (`app/`) and directly via `curl` against the bed at `10.0.0.1`. All writes were
reverted to the pre-test baseline afterward (firmness 28, manualprofile [26,16,18,26], mode manual).

- **`PUT /api/preferences/firmness`** - confirmed: value takes effect, visible in next `preferences` SSE event
  and in `GET /api/preferences`-equivalent state.
- **`PUT /api/preferences/manualprofile`** - confirmed: value is stored and reflected back in `preferences`.
  **Correction to earlier notes**: this does *not* directly update `/api/air/targets`. Setting
  `manualprofile` while idle left `air.targets` unchanged. The relationship between `firmness` /
  `manualprofile` / `air.targets` is still not fully understood - targets appear to get (re)computed from
  some combination of these only at specific moments (e.g. when `air/filling` is triggered), not on every
  preference write. Needs more observation to pin down exactly when/how `targets` updates.
- **`PUT /api/preferences/mode`** - confirmed: accepts `"manual"` (write path works; did not test switching to
  `"pressure"`/`"position"` live since those trigger automatic/different pump behavior).
- **`PUT /api/air/filling "true"`** (Fill) - confirmed: flips `filling` to `true`, and - new finding - sets
  `flows` to `["in","in","in","in"]` while actively pumping (previously only knew `"off"` as a valid flow
  value). Pressures did not visibly rise within ~10s of testing at low target deltas; needs a longer
  observation window to see the full fill curve.
- **`PUT /api/air/flows ["off","off","off","off"]`** (Stop) - confirmed: halts the pump (flows -> `off`,
  pressures stop changing) but does **not** clear the `filling` status flag by itself - `filling` stayed
  `true` until `/api/air/filling` was separately PUT `false`. **`bed_client.stop()` now sends both** (flows
  off + filling false) to fully match what a real "stop" should do; the original app's JS only ever sent
  `air/flows` off + a `/preferences/<position>` write, so this may be a behavior the original app never
  fully handled cleanly either.
- **SSE staleness caveat**: the bed does not appear to push a fresh `air` (or other) SSE event on every
  internal state change - after fixing `filling` via a direct `curl` PUT outside the app, our SSE-fed
  `/api/live` cache still showed the stale `filling:true` for some time after a direct `GET /api/air` against
  the bed already showed `false`. A dashboard relying solely on cached SSE state can lag reality; consider
  also polling `GET /api/air` directly on a timer rather than trusting SSE alone for this field.
