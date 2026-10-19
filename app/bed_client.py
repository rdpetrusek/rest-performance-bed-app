"""
Thin client for the ReST Bed local HTTP API.

Only wraps endpoints we have actually confirmed (either by observing the
official app's JS, or by testing read requests against a live bed). See
docs/protocol.md in the repo root for the full writeup. Endpoints that exist
per the firmware's string table but whose request bodies were never
exercised against real hardware are deliberately left out here rather than
guessed at.
"""
import httpx

DEFAULT_TIMEOUT = 8.0


class BedClient:
    def __init__(self, host: str):
        self.base = f"http://{host}/api"

    async def _get(self, path: str):
        async with httpx.AsyncClient(timeout=DEFAULT_TIMEOUT) as client:
            r = await client.get(f"{self.base}{path}")
            r.raise_for_status()
            return r.json()

    async def _put(self, path: str, body):
        async with httpx.AsyncClient(timeout=DEFAULT_TIMEOUT) as client:
            r = await client.put(f"{self.base}{path}", content=body)
            r.raise_for_status()
            return r

    # --- reads -------------------------------------------------------
    async def device(self):
        return await self._get("/device")

    async def values(self, key: str):
        return await self._get(f"/values/{key}")

    async def firmware_version(self):
        return await self._get("/firmware/version")

    # --- writes (confirmed shapes from the app bundle only) ----------
    async def set_mode(self, mode: str):
        """mode: 'manual' | 'pressure' | 'position'"""
        assert mode in ("manual", "pressure", "position")
        await self._put("/preferences/mode", f'"{mode}"')

    async def set_firmness(self, value: int):
        await self._put("/preferences/firmness", str(int(value)))

    async def set_manual_profile(self, zones: list[int]):
        assert len(zones) == 4
        body = "[" + ",".join(str(int(z)) for z in zones) + "]"
        await self._put("/preferences/manualprofile", body)

    async def start_filling(self):
        await self._put("/air/filling", "true")

    async def stop(self):
        """All zones off (the app's 'Stop' control)."""
        await self._put("/air/flows", '["off","off","off","off"]')
