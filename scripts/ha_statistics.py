#!/usr/bin/env python3
"""Read, repair and backfill Home Assistant long-term statistics.

Home Assistant keeps the hourly `statistics` table indefinitely, while `states`
and `statistics_short_term` are purged after `purge_keep_days`. Anything meant
to survive for years therefore has to exist as a statistics series, and this is
the tool for inspecting and correcting those series.

Every operation is a subcommand so that an import can be reviewed in a diff and
repeated, rather than typed once into a WebSocket console and forgotten. The
one thing worse than missing history is history nobody can account for.

What may be imported
--------------------
Only values that were genuinely measured, replayed from an independent record:
gas-meter index readings, the CSV written by sample_chaudiere.py, a series
recovered from an older instance.

Never interpolate, smooth or synthesise to fill a gap. Once imported, invented
rows are indistinguishable from measurements and will be analysed as real years
later. A hole in a chart is a correct statement that nothing was measured.

Anything computed rather than read belongs under its own external id, for
example `openv:burner_hours_estimated`, and never spliced into the entity
carrying real readings.

Credentials come from the environment, never from this file:

    export HA_URL=http://homeassistant.local:8123
    export HA_TOKEN=<long-lived access token>
"""

import argparse
import asyncio
import csv
import datetime as dt
import json
import os
import sys

try:
    import websockets
except ImportError:  # pragma: no cover - environment guidance
    sys.exit("this script needs the websockets package: pip install websockets")


class HomeAssistant:
    """A thin authenticated WebSocket client.

    The REST API does not expose the statistics commands, so everything here
    goes through the WebSocket API.
    """

    def __init__(self, url: str, token: str):
        self.url = url.replace("http://", "ws://").replace("https://", "wss://") + "/api/websocket"
        self.token = token
        self._id = 0

    async def __aenter__(self):
        self.ws = await websockets.connect(self.url, max_size=64 * 1024 * 1024)
        hello = json.loads(await self.ws.recv())
        if hello.get("type") != "auth_required":
            raise RuntimeError(f"unexpected greeting: {hello}")
        await self.ws.send(json.dumps({"type": "auth", "access_token": self.token}))
        ack = json.loads(await self.ws.recv())
        if ack.get("type") != "auth_ok":
            raise RuntimeError(f"authentication refused: {ack}")
        return self

    async def __aexit__(self, *_):
        await self.ws.close()

    async def call(self, **payload):
        self._id += 1
        await self.ws.send(json.dumps({"id": self._id, **payload}))
        while True:
            message = json.loads(await self.ws.recv())
            if message.get("id") == self._id and message.get("type") == "result":
                if not message.get("success"):
                    raise RuntimeError(f"{payload.get('type')} failed: {message.get('error')}")
                return message.get("result")


def hour_utc(when: dt.datetime) -> str:
    """Return an hour-aligned UTC timestamp.

    Home Assistant requires statistics rows to start exactly on an hour, and
    interprets them as UTC. This instance runs Europe/Paris, so a naive local
    timestamp lands one or two hours out depending on daylight saving — which
    is silent, and puts readings in the wrong month at a boundary.
    """
    if when.tzinfo is None:
        raise ValueError(f"{when!r} is naive; build timestamps as timezone-aware")
    return when.astimezone(dt.timezone.utc).replace(minute=0, second=0, microsecond=0).isoformat()


async def cmd_list(ha, args):
    """List the statistic ids Home Assistant knows about."""
    result = await ha.call(type="recorder/list_statistic_ids")
    rows = [r for r in result if args.filter in r["statistic_id"]]
    print(f"{len(rows)} statistic id(s) matching {args.filter!r}")
    for r in sorted(rows, key=lambda x: x["statistic_id"]):
        print(f"  {r['statistic_id']:<58} unit={r.get('display_unit_of_measurement')} "
              f"has_sum={r.get('has_sum')} source={r.get('source')}")


async def cmd_export(ha, args):
    """Export a series so it can be inspected before anything is changed.

    This is the step that makes a deletion reversible in the only sense that
    matters: the numbers are written down somewhere else first.
    """
    start = dt.datetime.fromisoformat(args.start).replace(tzinfo=dt.timezone.utc)
    end = dt.datetime.now(dt.timezone.utc)

    result = await ha.call(
        type="recorder/statistics_during_period",
        start_time=start.isoformat(),
        end_time=end.isoformat(),
        statistic_ids=[args.statistic_id],
        period=args.period,
    )

    rows = result.get(args.statistic_id, [])
    if not rows:
        print(f"no statistics for {args.statistic_id} since {args.start}")
        return

    print(f"{len(rows)} row(s) for {args.statistic_id}")
    fieldnames = ["start", "state", "sum", "min", "max", "mean"]
    writer = csv.DictWriter(sys.stdout if args.out == "-" else open(args.out, "w", newline=""),
                            fieldnames=fieldnames, extrasaction="ignore")
    writer.writeheader()
    for row in rows:
        start_value = row.get("start")
        if isinstance(start_value, (int, float)):
            start_value = dt.datetime.fromtimestamp(start_value / 1000, dt.timezone.utc).isoformat()
        writer.writerow({**row, "start": start_value})
    if args.out != "-":
        print(f"written to {args.out}")


async def cmd_clear(ha, args):
    """Drop a series entirely.

    Preferable to adjusting a corrupt one when the corruption is structural: a
    frozen plateau carries no information, and leaving it contaminates every sum
    computed across it.
    """
    if not args.yes:
        sys.exit("refusing to clear without --yes; export the series first")
    await ha.call(type="recorder/clear_statistics", statistic_ids=[args.statistic_id])
    print(f"cleared {args.statistic_id}")


async def cmd_import(ha, args):
    """Import measured rows from a CSV.

    The CSV must carry a `start` column of timezone-aware timestamps and either
    a `state`/`sum` pair, or a `mean` — matching what the series declares.
    """
    with open(args.csv, newline="") as handle:
        rows = list(csv.DictReader(handle))
    if not rows:
        sys.exit(f"{args.csv} holds no rows")

    stats = []
    for row in rows:
        when = dt.datetime.fromisoformat(row["start"])
        entry = {"start": hour_utc(when)}
        for key in ("state", "sum", "mean", "min", "max"):
            if row.get(key) not in (None, ""):
                entry[key] = float(row[key])
        stats.append(entry)

    metadata = {
        "statistic_id": args.statistic_id,
        "source": args.statistic_id.split(":")[0] if ":" in args.statistic_id else "recorder",
        "name": args.name,
        "unit_of_measurement": args.unit,
        "has_mean": any("mean" in s for s in stats),
        "has_sum": any("sum" in s for s in stats),
    }

    # An id containing a colon is an external statistic with no live entity
    # behind it; anything else must name an existing entity_id.
    if ":" not in args.statistic_id and metadata["source"] != "recorder":
        sys.exit("a statistic_id without a colon must use source 'recorder'")

    print(f"importing {len(stats)} row(s) into {args.statistic_id}")
    print(f"  {stats[0]['start']} .. {stats[-1]['start']}")
    if not args.yes:
        sys.exit("refusing to import without --yes; importing overwrites existing rows")

    await ha.call(type="recorder/import_statistics", metadata=metadata, stats=stats)
    print("imported")


async def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("list", help="list known statistic ids")
    p.add_argument("--filter", default="", help="substring to match")
    p.set_defaults(func=cmd_list)

    p = sub.add_parser("export", help="export a series to CSV before changing it")
    p.add_argument("statistic_id")
    p.add_argument("--start", default="2020-01-01T00:00:00")
    p.add_argument("--period", default="month", choices=["hour", "day", "week", "month"])
    p.add_argument("--out", default="-")
    p.set_defaults(func=cmd_export)

    p = sub.add_parser("clear", help="drop a series entirely")
    p.add_argument("statistic_id")
    p.add_argument("--yes", action="store_true")
    p.set_defaults(func=cmd_clear)

    p = sub.add_parser("import", help="import measured rows from a CSV")
    p.add_argument("statistic_id")
    p.add_argument("csv")
    p.add_argument("--name", required=True)
    p.add_argument("--unit", required=True)
    p.add_argument("--yes", action="store_true")
    p.set_defaults(func=cmd_import)

    args = parser.parse_args()

    url, token = os.environ.get("HA_URL"), os.environ.get("HA_TOKEN")
    if not url or not token:
        sys.exit("set HA_URL and HA_TOKEN in the environment")

    async with HomeAssistant(url, token) as ha:
        await args.func(ha, args)


if __name__ == "__main__":
    asyncio.run(main())
