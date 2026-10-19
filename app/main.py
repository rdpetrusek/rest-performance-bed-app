"""
ReST Bed local dashboard - self-hosted replacement for the dead vendor cloud.

Connects directly to the bed's local HTTP API (see docs/protocol.md), stores
every SSE event into a local SQLite database, and serves a small dashboard
for live state, history charts, basic controls, and an offline/deterministic
"last sleep session" summary. No cloud, no account, no external AI calls.
"""
import asyncio
import json
import os
import sqlite3
from contextlib import asynccontextmanager
from datetime import datetime, timedelta, timezone
from pathlib import Path

import httpx
from fastapi import FastAPI, HTTPException, Query
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel

from bed_client import BedClient

BED_HOST = os.environ.get("BED_HOST", "10.0.0.1")
DB_PATH = os.environ.get("DB_PATH", "/data/restbed.db")

bed = BedClient(BED_HOST)
latest: dict[str, dict] = {}  # most recent payload per SSE event name, in memory

# -----------------------------------------------------------------------
# storage
# -----------------------------------------------------------------------

def db_connect():
    Path(DB_PATH).parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        """CREATE TABLE IF NOT EXISTS events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            ts TEXT NOT NULL,
            event TEXT NOT NULL,
            data TEXT NOT NULL
        )"""
    )
    conn.execute("CREATE INDEX IF NOT EXISTS idx_events_event_ts ON events(event, ts)")
    return conn


def record_event(event: str, data) -> None:
    conn = db_connect()
    try:
        conn.execute(
            "INSERT INTO events (ts, event, data) VALUES (?, ?, ?)",
            (datetime.now(timezone.utc).isoformat(), event, json.dumps(data)),
        )
        conn.commit()
    finally:
        conn.close()


def query_events(event: str, start: datetime, end: datetime, limit: int = 5000):
    conn = db_connect()
    try:
        rows = conn.execute(
            "SELECT ts, data FROM events WHERE event = ? AND ts BETWEEN ? AND ? "
            "ORDER BY ts ASC LIMIT ?",
            (event, start.isoformat(), end.isoformat(), limit),
        ).fetchall()
        return [{"ts": ts, "data": json.loads(data)} for ts, data in rows]
    finally:
        conn.close()


# -----------------------------------------------------------------------
# SSE collector (connects to the bed, persists events, auto-reconnects)
# -----------------------------------------------------------------------

async def sse_collector():
    backoff = 2
    sse_url = f"http://{BED_HOST}/api/sse"
    while True:
        try:
            async with httpx.AsyncClient(timeout=None) as client:
                async with client.stream("GET", sse_url) as resp:
                    print(f"[collector] connected to {sse_url}")
                    backoff = 2
                    event_name = None
                    async for line in resp.aiter_lines():
                        if line.startswith("event:"):
                            event_name = line.split(":", 1)[1].strip()
                        elif line.startswith("data:") and event_name:
                            raw = line.split(":", 1)[1].strip()
                            try:
                                payload = json.loads(raw)
                            except json.JSONDecodeError:
                                payload = raw
                            if event_name != "time":
                                latest[event_name] = payload
                                record_event(event_name, payload)
                            event_name = None
        except Exception as exc:  # noqa: BLE001 - this loop must never die
            print(f"[collector] SSE connection lost: {exc}; retrying in {backoff}s")
        await asyncio.sleep(backoff)
        backoff = min(backoff * 2, 60)


@asynccontextmanager
async def lifespan(app: FastAPI):
    task = asyncio.create_task(sse_collector())
    yield
    task.cancel()


app = FastAPI(title="ReST Bed Dashboard", lifespan=lifespan)
app.mount("/static", StaticFiles(directory=Path(__file__).parent / "static"), name="static")


@app.get("/")
async def index():
    return FileResponse(Path(__file__).parent / "static" / "index.html")


# -----------------------------------------------------------------------
# live state
# -----------------------------------------------------------------------

@app.get("/api/live")
async def live():
    return latest


# -----------------------------------------------------------------------
# history
# -----------------------------------------------------------------------

VALID_EVENTS = {"device", "preferences", "body", "air", "surface", "temperature"}


@app.get("/api/history")
async def history(
    event: str = Query(..., description="one of: " + ", ".join(sorted(VALID_EVENTS))),
    hours: float = Query(24, ge=0.1, le=24 * 30),
    limit: int = Query(5000, ge=1, le=20000),
):
    if event not in VALID_EVENTS:
        raise HTTPException(400, f"event must be one of {sorted(VALID_EVENTS)}")
    end = datetime.now(timezone.utc)
    start = end - timedelta(hours=hours)
    return query_events(event, start, end, limit)


# -----------------------------------------------------------------------
# offline/deterministic sleep-session analysis (no LLM, no cloud)
# -----------------------------------------------------------------------

def _num(v, default=0.0):
    try:
        return float(v)
    except (TypeError, ValueError):
        return default


@app.get("/api/analysis/last-session")
async def last_session(
    lookback_hours: float = Query(36, ge=1, le=24 * 14),
    gap_minutes: float = Query(10, ge=1, le=120, description="max gap to bridge a brief presence flicker"),
):
    """
    Finds the most recent contiguous "present" session from the raw `body`
    events and summarizes it. Pure arithmetic over our own stored data -
    nothing leaves this machine.
    """
    end = datetime.now(timezone.utc)
    start = end - timedelta(hours=lookback_hours)
    rows = query_events("body", start, end, limit=50000)
    if not rows:
        return {"found": False, "reason": "no body events recorded in lookback window"}

    gap = timedelta(minutes=gap_minutes)
    sessions: list[list[dict]] = []
    current: list[dict] = []
    prev_ts = None
    for row in rows:
        present = bool(row["data"].get("present"))
        ts = datetime.fromisoformat(row["ts"])
        if present:
            if current and prev_ts is not None and ts - prev_ts > gap:
                sessions.append(current)
                current = []
            current.append(row)
            prev_ts = ts
        else:
            if current:
                sessions.append(current)
                current = []
            prev_ts = None
    if current:
        sessions.append(current)

    if not sessions:
        return {"found": False, "reason": "no 'present' session in lookback window"}

    session = sessions[-1]
    started = datetime.fromisoformat(session[0]["ts"])
    ended = datetime.fromisoformat(session[-1]["ts"])
    ongoing = bool(session[-1]["data"].get("present")) and (end - ended) < gap

    heart_rates = [_num(r["data"].get("heartrate")) for r in session if _num(r["data"].get("heartrate")) > 0]
    resp_rates = [
        _num(r["data"].get("breathing", {}).get("rate"))
        for r in session
        if _num(r["data"].get("breathing", {}).get("rate")) > 0
    ]
    moving_count = sum(1 for r in session if r["data"].get("moving"))

    def stats(values):
        if not values:
            return None
        return {"min": min(values), "max": max(values), "avg": sum(values) / len(values)}

    return {
        "found": True,
        "ongoing": ongoing,
        "start": session[0]["ts"],
        "end": session[-1]["ts"],
        "duration_minutes": round((ended - started).total_seconds() / 60, 1),
        "sample_count": len(session),
        "movement_events": moving_count,
        "movement_ratio": round(moving_count / len(session), 3) if session else None,
        "heart_rate": stats(heart_rates),
        "respiration_rate": stats(resp_rates),
    }


# -----------------------------------------------------------------------
# controls (only endpoints whose request bodies were confirmed - see
# docs/protocol.md; this is physical hardware, keep this list conservative)
# -----------------------------------------------------------------------

class ModeBody(BaseModel):
    mode: str  # manual | pressure | position


class FirmnessBody(BaseModel):
    value: int


class ProfileBody(BaseModel):
    zones: list[int]


@app.post("/api/control/mode")
async def control_mode(body: ModeBody):
    await bed.set_mode(body.mode)
    return {"ok": True}


@app.post("/api/control/firmness")
async def control_firmness(body: FirmnessBody):
    await bed.set_firmness(body.value)
    return {"ok": True}


@app.post("/api/control/manual-profile")
async def control_manual_profile(body: ProfileBody):
    if len(body.zones) != 4:
        raise HTTPException(400, "zones must have exactly 4 values")
    await bed.set_manual_profile(body.zones)
    return {"ok": True}


@app.post("/api/control/fill")
async def control_fill():
    await bed.start_filling()
    return {"ok": True}


@app.post("/api/control/stop")
async def control_stop():
    await bed.stop()
    return {"ok": True}
