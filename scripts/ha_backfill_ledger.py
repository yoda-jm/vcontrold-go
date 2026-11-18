#!/usr/bin/env python3
"""Rebuild Home Assistant's hourly statistics from the daemon's ledger.

When readings cannot reach Home Assistant — broker unreachable, Home Assistant
down — the daemon writes them to `ledger/YYYY-MM-DD.csv` beside its
configuration, with the hour before the failure was noticed. This script turns
those readings into the hourly long-term statistics Home Assistant would have
compiled itself, and imports them.

Why statistics and not states
-----------------------------
Home Assistant offers no way to insert past states, and replaying the readings
over MQTT would stamp them all with the time of the replay. Hourly statistics
can be imported, and they are what lasts: states are purged after
`purge_keep_days`, after which the history panel itself falls back on them.

What is imported
----------------
Only hours with no row in Home Assistant: an existing row is never overwritten.

* Counters (`has_sum`): the last reading of each hour. The running sum is
  anchored on Home Assistant's own rows around the gap, and the import is
  refused if they disagree: that would mean Home Assistant recorded a reset.
* Measurements (`mean`): time-weighted mean, min and max, only for hours whose
  readings span at least 40 minutes. A sparser hour stays empty rather than
  being estimated from a few points.
* Nothing from the last two hours, which Home Assistant may not have compiled
  yet.

Entities without statistics (binary sensors, text) are skipped: Home
Assistant keeps nothing long-term for them.

    export HA_URL=http://homeassistant.local:8123
    export HA_TOKEN=<long-lived access token>
    scp target:vcontrold-go/ledger/2026-11-17.csv .
    ./ha_backfill_ledger.py 2026-11-17.csv            # dry run
    ./ha_backfill_ledger.py 2026-11-17.csv --yes      # import
"""

import argparse
import asyncio
import csv
import datetime as dt
import json
import os
import re
import sys
import unicodedata
from collections import defaultdict

try:
    import websockets
except ImportError:  # pragma: no cover - environment guidance
    sys.exit("this script needs the websockets package: pip install websockets")

# internal/mqtt/entity.go: unique_id is "<device>_<slugified alias>".
DEVICE_IDENTIFIER = "vitola200"
MIN_SPAN = dt.timedelta(minutes=40)
HOUR = dt.timedelta(hours=1)
# Home Assistant compiles an hour shortly after it ends. An hour that recent may
# simply not be compiled yet: importing it would race the recorder.
SETTLE = dt.timedelta(hours=2)


def slugify(label):
    """Mirror slugify() in internal/mqtt/entity.go."""
    folded = unicodedata.normalize("NFKD", label.lower()).encode("ascii", "ignore").decode()
    return re.sub(r"[^a-z0-9]+", "_", folded).strip("_")


def read_ledger(paths):
    """Return {alias: [(time, value), ...]} sorted by time."""
    readings = defaultdict(list)
    for path in paths:
        with open(path, newline="") as handle:
            for row in csv.DictReader(handle):
                readings[row["alias"]].append((dt.datetime.fromisoformat(row["time"]), row["value"]))
    return {alias: sorted(set(points)) for alias, points in readings.items()}


def hour_start(when):
    return when.astimezone(dt.timezone.utc).replace(minute=0, second=0, microsecond=0)


def counter_rows(points, offset):
    """Last reading of each hour, as state and sum."""
    last = {}
    for when, value in points:
        last[hour_start(when)] = float(value)
    return {h: {"state": v, "sum": round(v - offset, 6)} for h, v in last.items()}


def mean_rows(points):
    """Time-weighted mean, min and max of each sufficiently covered hour."""
    by_hour = defaultdict(list)
    for when, value in points:
        by_hour[hour_start(when)].append((when, float(value)))
    rows = {}
    for h, pts in by_hour.items():
        if pts[-1][0] - pts[0][0] < MIN_SPAN:
            continue
        # Each reading holds until the next one, the last until the hour ends.
        ends = [t for t, _ in pts[1:]] + [h + HOUR]
        weights = [(end - t).total_seconds() for (t, _), end in zip(pts, ends)]
        mean = sum(v * w for (_, v), w in zip(pts, weights)) / sum(weights)
        rows[h] = {"mean": round(mean, 3), "min": min(v for _, v in pts), "max": max(v for _, v in pts)}
    return rows


class HomeAssistant:
    def __init__(self, url, token):
        self.url = url.replace("http://", "ws://").replace("https://", "wss://").rstrip("/") + "/api/websocket"
        self.token = token
        self._id = 0

    async def __aenter__(self):
        self.ws = await websockets.connect(self.url, max_size=64 * 1024 * 1024)
        await self.ws.recv()
        await self.ws.send(json.dumps({"type": "auth", "access_token": self.token}))
        reply = json.loads(await self.ws.recv())
        if reply.get("type") != "auth_ok":
            sys.exit(f"authentication failed: {reply}")
        return self

    async def __aexit__(self, *_):
        await self.ws.close()

    async def call(self, **payload):
        self._id += 1
        await self.ws.send(json.dumps({"id": self._id, **payload}))
        while True:
            reply = json.loads(await self.ws.recv())
            if reply.get("id") == self._id:
                if not reply.get("success"):
                    raise RuntimeError(f"{payload['type']}: {reply.get('error')}")
                return reply.get("result")


async def backfill(ha, readings, apply):
    registry = await ha.call(type="config/entity_registry/list")
    by_unique = {e["unique_id"]: e["entity_id"] for e in registry if e.get("platform") == "mqtt"}

    targets = {}
    for alias in readings:
        entity = by_unique.get(f"{DEVICE_IDENTIFIER}_{slugify(alias)}")
        if entity:
            targets[alias] = entity
        else:
            print(f"  {alias}: no Home Assistant entity, skipped")
    if not targets:
        return

    metadata = {m["statistic_id"]: m for m in await ha.call(
        type="recorder/get_statistics_metadata", statistic_ids=list(targets.values()))}

    first = min(p[0][0] for p in readings.values())
    last = max(p[-1][0] for p in readings.values())
    existing = await ha.call(
        type="recorder/statistics_during_period", statistic_ids=list(metadata), period="hour",
        start_time=(first - dt.timedelta(days=3)).isoformat(), end_time=(last + dt.timedelta(days=1)).isoformat(),
        types=["state", "sum", "mean"])

    for alias, entity in sorted(targets.items(), key=lambda kv: kv[1]):
        meta = metadata.get(entity)
        if meta is None:
            print(f"  {entity}: no long-term statistics, skipped")
            continue
        rows_ha = {dt.datetime.fromtimestamp(r["start"] / 1000, dt.timezone.utc): r for r in existing.get(entity, [])}
        points = readings[alias]

        if meta["has_sum"]:
            before = [h for h in rows_ha if h < hour_start(points[0][0])]
            after = [h for h in rows_ha if h > hour_start(points[-1][0])]
            if not before:
                print(f"  {entity}: no row before the gap to anchor the sum on, skipped")
                continue
            anchor = rows_ha[max(before)]
            offset = anchor["state"] - anchor["sum"]
            if after:
                later = rows_ha[min(after)]
                if abs((later["state"] - later["sum"]) - offset) > 1e-6:
                    print(f"  {entity}: Home Assistant recorded a reset across the gap, skipped")
                    continue
            rows = counter_rows(points, offset)
        elif meta.get("has_mean") or meta.get("mean_type"):
            rows = mean_rows(points)
        else:
            print(f"  {entity}: neither sum nor mean, skipped")
            continue

        settled = dt.datetime.now(dt.timezone.utc) - SETTLE
        new = {h: r for h, r in rows.items() if h not in rows_ha and h + HOUR <= settled}
        kept = len(rows) - len(new)
        span = f"{min(new):%d/%m %H:%M} .. {max(new):%d/%m %H:%M} UTC" if new else "nothing"
        print(f"  {entity}: {len(new)} hour(s) to import ({span})"
              + (f", {kept} already in Home Assistant or too recent, left alone" if kept else ""))
        if not new or not apply:
            continue

        md = {k: meta[k] for k in ("statistic_id", "source", "name", "has_mean", "mean_type", "has_sum", "unit_class")
              if k in meta}
        md["unit_of_measurement"] = meta.get("statistics_unit_of_measurement")
        stats = [{"start": h.isoformat(), **r} for h, r in sorted(new.items())]
        await ha.call(type="recorder/import_statistics", metadata=md, stats=stats)
        print("    imported")


async def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("ledger", nargs="+", help="ledger CSV files written by the daemon")
    parser.add_argument("--yes", action="store_true", help="import; without it, only show what would be")
    args = parser.parse_args()

    url, token = os.environ.get("HA_URL"), os.environ.get("HA_TOKEN")
    if not url or not token:
        sys.exit("set HA_URL and HA_TOKEN in the environment")

    readings = read_ledger(args.ledger)
    print(f"{sum(len(p) for p in readings.values())} reading(s) for {len(readings)} alias(es)"
          + ("" if args.yes else " — dry run, add --yes to import"))
    async with HomeAssistant(url, token) as ha:
        await backfill(ha, readings, args.yes)


if __name__ == "__main__":
    asyncio.run(main())
