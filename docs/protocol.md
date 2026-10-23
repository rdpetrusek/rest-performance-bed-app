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

## `air.targets` investigation (2026-10-19)

Both of the user's two beds (`ReST-ee0d07` = P3A "left", `publ5z65ha`; `ReST-ee0eb9` = P3A "right",
`pubr5z65ha` - a matched dual-zone pair, per their `l`/`r` configuration codes) are now provisioned into
indirect mode and reachable on the home LAN (`.240` and `.239` respectively). `ee0d07` is a real, in-use bed
(sensor pressures show live jitter; user physically confirmed air in it after a Fill); `ee0eb9` is currently
not connected to a bladder/pad (pressure readings were bit-for-bit frozen across a 60s poll - no sensor
noise at all, unlike `ee0d07`).

**Motivating symptom**: Fill audibly worked once on `ee0d07` (pump kicked on, bed physically filled), but
every later Fill attempt - after setting firmness, manualprofile, and both `"manual"` and `"position"` mode -
did nothing. Root cause isolated to `air.targets` being stuck at `[20,20,20,20]` regardless of any
preference write, while `air.pressures` was already above that value (so Fill, which appears to mean "pump
until pressure reaches target," correctly had nothing left to do).

**Confirmed via Ghidra (headless, against `tools/out/p3-stable.elf`)**, not just live testing:
- `PUT /preferences/{back,side,manual}profile` are handled by three near-identical setter functions
  (`FUN_1f00aecc`, `FUN_1f00af60`, `FUN_1f00aff4` in this build) that copy the new 4-int array into the live
  preference variable and persist each element to flash (per-zone keys, e.g. `manualProfile0..3`) - **none of
  them write anything resembling `targets`**. This is a hard confirmation, from the actual firmware code, of
  what live testing already suspected: setting the manual profile never updates `air.targets`.
- `FUN_1f00abd4` loads `backProfile0..3`/`sideProfile0..3`/`manualProfile0..3` from flash into the live
  preference variables - this has the shape of a boot-time initializer (reads persisted config with
  fallback defaults), not something called per-request.
- The literal JSON field names `filling`/`targets`/`flows`/`pressures` do not exist anywhere in the firmware
  as strings - only the route paths (`/api/air/filling` etc.) do. The `/api/air` response is therefore built
  by a generic, table/descriptor-driven JSON framework (common in these embedded HTTP SDKs), not per-field
  printf-style code - so there is no single findable "compute targets" function to grep for. Confirming what
  actually sets `targets` would need reverse-engineering that generic framework's descriptor tables, a much
  larger undertaking than everything above.

> **Superseded (2026-10-19, later):** the real cause of everything in this section was a disconnected
> Boditrak sensor pad - see "Root cause: sensor pad gates all air control" below. With the pad
> connected, `targets` follows `manualprofile` live, with no reboot. The boot-time theory below was
> built on observations made while the pad was disconnected and should not be relied on.

**Working theory (unconfirmed)**: `air.targets` is computed once at boot from whatever `manualprofile` (or
related preferences) are stored at that time, not recomputed live on preference writes. Supporting evidence:
the firmware's own preference-loading function runs at boot, not per-request; `ee0eb9`'s `manualprofile` has
apparently never been touched and sits at exactly `[20,20,20,20]`, matching its `targets` exactly, while
`ee0d07`'s `manualprofile` was pushed to `[45,45,45,45]` live and its `targets` stayed at the old `[20,...]`
value, consistent with "last computed before our changes."

**Confirmed** (not just theorized): set `ee0eb9`'s `manualprofile` to `[45,45,45,45]` (while its `targets`
was still `[20,20,20,20]`), power-cycled it, and `targets` came back as `[45,45,45,45]` - exactly matching
the profile set before the reboot. `air.targets` is computed once at boot from the stored manual profile,
confirmed by a clean before/after test, not recomputed live on preference writes. (A first power-cycle on
`ee0eb9`, done before its profile was changed from the matching default `[20,20,20,20]`, was inconclusive on
this specific question but did confirm indirect-mode Wi-Fi and the `filling` flag both survive/reset sanely
across a reboot.)

**New wrinkle**: the moment `air/filling` is triggered, `targets` changes again - and not to the
boot-computed profile value. Right after the reboot above, `targets` was `[45,45,45,45]`; triggering Fill
immediately reset it to `[20,20,20,20]` on `ee0eb9`, and to `[30,30,30,30]` on `ee0d07` (a third, distinct
value from any profile either bed had stored). So `air/filling` appears to compute its own target from
something other than `manualprofile` - still unidentified. This means every fill observed all day (both
beds, every profile/firmness/mode tried) was converging on whatever Fill itself computes, not the user's
chosen profile - consistent with the original successful fill on `ee0d07` having settled around
pressure ~23-27 regardless of its profile.

### Wi-Fi mode ruled out as the cause of "Fill does nothing" (2026-10-19)

`ee0d07`'s Fill worked once (audible pump, physically confirmed air in the bed) before it was provisioned
into indirect mode; every attempt since has produced no audible pump activity. To test whether indirect mode
itself was the cause, `ee0d07` was switched back to direct mode (`PUT /wifi/mode "direct"`) and Fill was
retried while physically listening - **still no pump sound**, same as in indirect mode. Network mode is
therefore ruled out as the explanation. (The same direct-mode retest on `ee0eb9` also produced no sound, but
that bed was never a valid test case for this question - it has no bladder/pump module connected at all,
confirmed separately by its pressure sensor readings being completely frozen bit-for-bit across a 60s poll,
unlike `ee0d07`'s, which show real jitter.)

Leading theory now: repeated preference writes and fill start/stops on `ee0d07` without a single power cycle
all day may have wedged the pump/valve control task - `ee0eb9` was power-cycled twice tonight and behaved
consistently each time; `ee0d07` has not been power-cycled once. Not yet tested (physically inconvenient to
reach).

### Root cause: sensor pad gates all air control (2026-10-19, confirmed by repair)

**The pump/valves do nothing unless the Boditrak USB sensor pad is connected and reporting.** Fill
returns 204 but never starts (`filling` stays `false`, `flows` stay `off`, no `Pump = %s` log line), and
`targets` stops tracking `manualprofile`. No API call works around it.

Fixed physically on `ee0d07`: unzipped the mattress cover, reseated the pad's USB cable at the pump
box (unplug/replug, wiggle) until it connected. Immediately afterward, without a reboot,
`targets` went from the stale `[30,30,30,30]` to the stored `manualprofile` `[10,10,10,10]` and the
valves opened and let air out.

How to diagnose it from the API (all read-only):
- `GET /api/log` (plain-text firmware log ring buffer) repeats `Attempting to connect to sensor...`
  while the pad is disconnected; once it connects that stops and `Added event to memory and storage`
  lines appear.
- `GET /api/surface` has `rows`/`columns` `0` and empty `pressures` while disconnected; connected, it
  returned `columns:9, rows:31, width:482, height:1473, units:"mmHg"`.
- `body.position` is `"unknown"` while disconnected, `"empty"` once the pad is up and nobody's on it.

A flaky connector is the likely failure mode: the pad worked once (the first successful Fill), then
dropped out, and it took repeated reseating to come back.

Other endpoints confirmed live the same day (GET only):
- `GET /api/air/gauges` - raw per-zone ADC counts, e.g. `[5074,3871,3729,2212]`.
- `GET /api/air/cal` - per-zone `[offset, scale]` pairs, e.g. `[3008,0.012, 1802,0.012, 1953,0.013, 38,0.013]`.
  `pressures[z] = trunc((gauges[z] - offset[z]) * scale[z])` - verified exactly for all 4 zones.
  (Zone 4's offset of 38 vs ~1800-3000 for the others looks like a suspect calibration.)
- `GET /api/preferences/cyclevalves` -> `true`; `GET /api/uptime` -> `"H:MM:SS"`; `/api/monitor` is 404 on P3A.
- Firmware strings `Pump = %s`, `Exiting fill mode`, `pumptime` exist and should show up in `/api/log`
  during a fill.

Preference writes outside `minimum`..`maximum` (5..50) are accepted without error (seen: `firmness` 92,
`manualprofile` `[1,1,1,1]`), so the client has to clamp.

### How the firmware gates air control on the pad (Ghidra, `p3-stable.elf`, 2026-10-19)

Script: `tools/ghidra-scripts/DecompRefs.java` (decompiles functions by string, address, pointer, or caller).

- **Main control loop** `FUN_1f00a288` (every 100 ms) calls `FUN_1f00c080` (read one pad frame) first; if it
  returns 0 it calls `FUN_1f007d74` and skips the rest of the cycle - target computation, fill start/stop,
  pump and valve drive. That is the gate.
- `FUN_1f00c080` **returns true without touching the pad when the bed type is 4-7.** Bed type comes from the
  **`configuration`** string (persisted under flash key `configuration`), mapped by `FUN_1f008284`:

  | configuration | type | pad |
  |---|---|---|
  | `publ5z65ha` / `pubr5z65ha` | 0 / 1 | required (5-zone left/right - the user's two beds) |
  | `pibl3z49ha` / `pibr3z49ha` | 2 / 3 | required (3-zone) |
  | `publ5z00ha` / `pubr5z00ha` | 4 / 5 | **never read** |
  | `pibl3z00ha` / `pibr3z00ha` | 6 / 7 | **never read** |

  `z65`/`z49` identify the pad; `z00` = no pad (matches the app JS testing configs for an `00ha` suffix).
  Types 1 and 3 mirror the pad's columns (right-side beds). Switching to a type 4-7 also resets one other
  persisted value to 2 (`FUN_1f008214`). In "pressure" (automatic) mode a sensorless type uses a fallback
  that holds around `firmness`, since there is no body map.
- `PUT /api/device/configuration "<string>"` is wrapped as `bed.Client.SetConfiguration`, restricted to the
  eight strings above. **Not yet exercised against a live bed.**

**Pad wire protocol** ("FSA" - Vista Medical's force-sensing array), from `FUN_1f00ef5c`: ASCII-framed
request/response over the pad's USB link. A frame is `FSA:` + number of length digits + `:` + length + `.` +
payload, e.g. `FSA:1:7.columns`. Commands seen: `columns`, `rows`, `width`, `height` (replies parsed as
decimal integers), `scan` (reply = rows×columns little-endian int16 readings; >0xffe = saturated),
`caldate`, `calsize`, `caltime`, `calsector`. USB side: `[usbvmusc]` "Vista Medical USB Class" driver,
accepted VID/PID table at `*(0x1f054104)` (not yet dumped).

### Exact calls, verified against the real app's own code (2026-10-19)

The following is independent verification, not a restatement of this file's own earlier notes - extracted
directly from the official app's bundled web assets (`out/apktool/assets/public/assets/index-b3d7c305.js`,
a Capacitor/webview app bundle; minified, one line per module). Confirmed byte-for-byte identical to
`internal/bed/client.go`'s request bodies.

```js
// Fill - PUT /api/air/filling, body "true" (bare, unquoted => JSON boolean)
tO = async e => {
  try {
    const t = `http://${e.address}/api/air/filling`;
    await fetch(t, {body: "true", cache: "no-cache", method: "PUT", referrer: "no-referrer"})
  } catch(t) { console.error(t) }
}

// Stop (zero flows) - PUT /api/air/flows, body ["x","x","x","x"] (quoted strings)
oO = async (e, t) => {
  try {
    const n = `http://${e.address}/api/air/flows`,
          r = `["${t[0]}","${t[1]}","${t[2]}","${t[3]}"]`;
    await fetch(n, {body: r, cache: "no-cache", method: "PUT", referrer: "no-referrer"})
  } catch(n) { console.error(n) }
}

// Manual profile - PUT /api/preferences/manualprofile, body [n,n,n,n] (unquoted numbers)
og = async (e, t) => {
  try {
    const n = `http://${e.address}/api/preferences/manualprofile`,
          r = `[${t[0]},${t[1]},${t[2]},${t[3]}]`;
    await fetch(n, {body: r, cache: "no-cache", method: "PUT", referrer: "no-referrer"})
  } catch(n) { console.error(n) }
}

// Generic profile setter (also used for backprofile/sideprofile - same array shape as manualprofile,
// never implemented in our client since never tested live)
AS = async (e, t, n) => {
  try {
    const r = `http://${e.address}/api/preferences/${t.toString()}`,
          o = `[${n[0]},${n[1]},${n[2]},${n[3]}]`;
    await fetch(r, {body: o, cache: "no-cache", method: "PUT", referrer: "no-referrer"})
  } catch(r) { console.error(r) }
}

// Firmness - PUT /api/preferences/firmness, body is the bare number, no quotes/brackets
T9 = async (e, t) => {
  try {
    const n = `http://${e.address}/api/preferences/firmness`;
    await fetch(n, {body: `${t}`, cache: "no-cache", method: "PUT", referrer: "no-referrer"})
  } catch(n) { console.error(n) }
}

// Mode - PUT /api/preferences/mode, body "manual"/"position"/"pressure" (quoted string).
// Appears twice in the bundle under different minified names (nO, EW) - same body, likely
// duplicated across two bundle chunks rather than two different call sites.
nO = async (e, t) => {
  try {
    const n = `http://${e.address}/api/preferences/mode`;
    await fetch(n, {body: `"${t.toString()}"`, cache: "no-cache", method: "PUT", referrer: "no-referrer"})
  } catch(n) { console.error(n) }
}
```

No `Content-Type` header is set on any of these (matches `cleartext`/unset-content-type behavior noted
elsewhere in this doc) and none set anything beyond `cache`/`method`/`referrer`/`body`. There is no hidden
extra step, header, or parameter in the real app's Fill/Stop/profile/firmness/mode calls beyond what's
already in `internal/bed/client.go`.
