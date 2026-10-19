# ReST Bed hardware - what we know from the firmware

Both companies behind this product are defunct: **Boditrak** (pressure-sensing pad technology,
and the firmware OTA distribution point) was a subsidiary of **Vista Medical** (the pump-box
software/firmware maintainer, `grant@vista-medical.com`), and both went bankrupt. There is no
vendor support path left - the local HTTP API documented in `protocol.md` is the only way to
operate this hardware going forward.

Three distinct hardware generations exist across the product line, identified from the three
firmware images bundled in the Android app (`firmware/`, extracted/analyzed in `Dockerfile.fw`
via `restbed-fw`). Your bed (model code `mac`, both the unit tested directly and its matching
side, config `publ5z65ha`/`pubr5z65ha` - almost certainly left/right of one split/dual-zone bed)
is the middle generation, **P3A on a Marvell 88MW300**.

## P1 - oldest generation: embedded Linux (`p1-stable.deb`)
- ARM (armhf), Debian-wheezy-era Linux, OEM package `smartbed` built by Vista Medical
  (`grant@vista-medical.com`).
- Main daemon `/usr/sbin/smartbed` (stripped ELF) implements the whole `/api` HTTP server;
  separate unstripped binary `/opt/smartbed/vitals` reads the Boditrak pad over USB with live
  hot-plug support (`onSensorsChanged`, `"New Sensor is connected..."`).
- Valve/pump hardware driven over **SPI** (`/dev/spidev0.0`, `/dev/spidev0.1`; log strings
  `Opened/Closed/Locked/Unlocked valve %d`, `Start/Completed valve cycling`).
- Wi-Fi AP/client switching via `hostapd`/`wpa_supplicant`/`dhclient` shell scripts
  (`setwifimode`), almost certainly over an external USB Wi-Fi(+Bluetooth) combo dongle
  (`bluez`, `libusb-1.0` dependencies; kernel drivers live in the base OS image we don't have,
  not in this package). **This is the generation where a flaky USB connector plausibly does
  take out Wi-Fi** - unlike the Marvell/ESP32 generations, which have on-chip radios.
- `rc.local` wipes `/etc/wifimode` on every boot and derives the `ReST-<id>` hostname/SSID from
  `wlan0`'s MAC address every time.
- Ships its own cron-driven telemetry uploader (`sbuploader.py`, every minute) straight to
  `http://www.vista-medical.com/rest/api/v1/events` in plaintext, independent of any Azure path.
- OTA firmware comes from `http://www.boditrak.com/software/rest/pump/%s/smartbed_%s_armhf.deb`
  - i.e. Boditrak hosted firmware distribution for Vista Medical's product.

## P2 / P3 / P3A - your bed's generation: Marvell 88MW300 (`p3-stable.bin`)
- Confirmed via embedded strings: `Marvell-WM`, `WMSDK_4.0`, SDK source paths such as
  `sdk/src/drivers/mw300/mdev_i2c.c`, `sdk/src/wlan/wifidriver/wifi-uap.c`.
- **Chip**: Marvell 88MW300 (later divested to NXP as part of Marvell's connectivity business) -
  an ARM Cortex-M4F core with 802.11 b/g/n Wi-Fi integrated on-die. The radio is not a USB
  dongle on this generation.
- **RTOS/SDK**: FreeRTOS via Marvell's WMSDK 4.0, including its own micro-AP Wi-Fi mode
  (`wifi-uap.c` - this is literally what broadcasts `ReST-<id>`) and its own lightweight HTTP
  server/client (`httpd.c`/`httpc.c` - implements the whole `/api` surface and the `/api/sse`
  stream), mbedTLS for crypto.
- **USB**: the SDK bundles its own `usb-host` stack - the chip itself is a USB *host* and talks
  to the Boditrak pad directly as an attached USB device. Confirmed live on two separate units
  (both broadcasting Wi-Fi fine with `/api/surface` empty, i.e. no sensor pad actively
  connected) that Wi-Fi does **not** depend on this USB connection on this generation.
- Other on-chip peripheral drivers present: I2C, UART, DMA. No GPIO/SPI driver strings
  surfaced for this chip, suggesting valve/pump/pressure-transducer hardware is likely driven
  over **I2C** (possibly an I2C valve-driver IC / ADC combo) rather than SPI like the P1 board.
- **Cloud**: `azure_iot_client` string present (Azure IoT Hub), but no MQTT strings - likely the
  HTTPS-based Azure IoT device SDK rather than MQTT (unlike the newest generation below).
- Live-confirmed HTTP header: the bed actually serves `Server: ReST SmartBed` (a custom string
  overriding the WMSDK default `Marvell-WM`, which is what's still sitting in the binary as the
  SDK's hardcoded fallback - that's how the chip was identified).
- Model codes: `maa` = P2, `mab` = P3, `mac` = P3A (confirmed to be the model in this bed).
  Firmware file `p3-stable.bin`, version string seen live: `0.08.001` (latest bundled: `0.08.003`).

## P3B / P3C / P3D - newest generation: Espressif ESP32-S2 (`p3b-stable.bin`)
- Confirmed via `file`'s own magic-byte detection (a real ESP-IDF app image) plus
  `IDF/components/.../esp32s2/...` source paths. Project name `rest_espressif`, built
  2023-04-25, firmware version string `1.08.039-1-ga5a8c0b-dirty`, ESP-IDF `v4.4-dev-2184-g166c30e7b2`.
- Full Xtensa ESP32-S2, FreeRTOS via ESP-IDF.
- Richer peripheral set visible directly in strings: GPIO, SPI (flash + likely the
  valve/sensor bus), two ADC channels (`ADC_NUM_1`/`ADC_NUM_2` - almost certainly the per-zone
  pressure-transducer inputs), and a notably more sophisticated calibration-data subsystem for
  the Boditrak pad: MAC-keyed, checksummed calibration blobs persisted in flash (NVS), with
  fallback-to-full-calibration handling (`"Sensor connected without calibration"`,
  `"Loaded sensor calibration"`, `"calibration data MAC check failed"`, etc.) - nothing this
  elaborate shows up in either older generation's strings.
- **Cloud moved to MQTT over TLS** straight to `*.azure-devices.net` (`esp_mqtt_client_init`,
  string `azure-devices`) - a shift from the older generations' HTTPS-based approach. Both
  mbedTLS and wolfSSL error strings are present, possibly a transitional/dual-stack build.
- Model codes: `eaa` = P3B, `eab` = P3C, `eac`/`ead` = P3D.

## Common architecture across all three generations
- Local HTTP REST API + Server-Sent-Events stream on port 80, **no authentication at all** -
  confirmed both from firmware strings (no token/secret checks anywhere) and live testing
  (every GET/PUT answered an anonymous client).
- Hostname/AP SSID is always `ReST-<id>`, derived from the board's own MAC address.
- Boots into its own open Wi-Fi AP by default; can be switched to join a home network, but on
  the P1 generation that join is the fragile, drop-prone path - the Marvell and ESP32
  generations' on-chip radios don't share that specific failure mode.
- Cloud link (when enabled) is Azure IoT Hub, provisioned via Vista Medical's now-dead Azure
  Functions (`rest-bed-registration`, `rest-sleep-events-register/retrieve`) and Auth0 tenant
  (`rest-bed.auth0.com`) - all unreachable now that the company is gone.
- Firmware OTA is a simple `PUT /api/firmware` with the raw image body on every generation.

See `docs/protocol.md` for the full REST/SSE API reference this all exposes.
