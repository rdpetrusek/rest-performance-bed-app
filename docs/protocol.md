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
