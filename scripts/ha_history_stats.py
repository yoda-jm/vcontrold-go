#!/usr/bin/env python3
"""Create Home Assistant history_stats helpers over the REST config-flow API.

Why these exist
---------------
A text sensor has no long-term memory in Home Assistant. `getBetriebArtM1`
publishes WW / RED / NORM / H+WW / ABSCHALT, which lives in the `states` table
and is purged after `purge_keep_days`.

The numeric mirror published by the daemon (`openv/getBetriebArtM1_code`) is
what actually guarantees permanent history. These helpers are the complement:
they answer the question people actually ask — "how many days were we in
heating mode last winter" — directly, without post-processing an hourly
min/max series.

Each helper resets at midnight, so the daily maximum of its statistic is that
day's hours in the mode.

Credentials come from the environment:

    export HA_URL=http://homeassistant.local:8123
    export HA_TOKEN=<long-lived access token>
"""

import json
import os
import sys
import urllib.error
import urllib.request

# The names are deliberate and final. Renaming a helper renames its entity,
# which orphans the statistics keyed on that entity_id and starts an empty
# series — the one mistake this whole exercise is meant to avoid.
HELPERS = [
    # The operating mode, one helper per state. The mode is the reference every
    # consumption analysis is calibrated against: it says when a heating season
    # actually started and stopped.
    ("Mode NORM aujourd'hui", "sensor.vitola_200_mode_de_fonctionnement", "NORM"),
    ("Mode RED aujourd'hui", "sensor.vitola_200_mode_de_fonctionnement", "RED"),
    ("Mode WW aujourd'hui", "sensor.vitola_200_mode_de_fonctionnement", "WW"),
    ("Mode ABSCHALT aujourd'hui", "sensor.vitola_200_mode_de_fonctionnement", "ABSCHALT"),
]


def request(url, token, path, payload=None):
    req = urllib.request.Request(
        url + path,
        data=json.dumps(payload).encode() if payload is not None else None,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        method="POST" if payload is not None else "GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            return json.loads(response.read())
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"{path}: {exc.code} {exc.read().decode()[:200]}") from exc


def existing_helper_names(url, token):
    """Return the friendly names of the history_stats helpers already present.

    Creating a duplicate would produce a second entity with a suffixed id and
    two half-populated series, so this script has to be safe to re-run.
    """
    entries = request(url, token, "/api/config/config_entries/entry")
    return {e["title"] for e in entries if e.get("domain") == "history_stats"}


def create(url, token, name, entity_id, state):
    """Walk the config flow.

    Three steps, and the schema of each is only discoverable by submitting the
    previous one: name and entity, then the state to match, then the window.
    """
    flow = request(url, token, "/api/config/config_entries/flow",
                   {"handler": "history_stats", "show_advanced_options": True})
    path = f"/api/config/config_entries/flow/{flow['flow_id']}"

    step = request(url, token, path,
                   {"name": name, "entity_id": entity_id, "type": "time"})

    if step.get("step_id") == "state":
        step = request(url, token, path, {"entity_id": entity_id, "state": [state]})

    if step.get("type") == "form":
        # Reset at midnight, so the daily maximum of the statistic is that
        # day's hours in the state.
        step = request(url, token, path,
                       {"start": "{{ today_at() }}", "end": "{{ now() }}"})

    if step.get("type") != "create_entry":
        raise RuntimeError(f"{name}: flow ended as {step.get('type')} "
                           f"step={step.get('step_id')} errors={step.get('errors')}")
    return step


def main():
    url, token = os.environ.get("HA_URL"), os.environ.get("HA_TOKEN")
    if not url or not token:
        sys.exit("set HA_URL and HA_TOKEN in the environment")

    present = existing_helper_names(url, token)
    for name, entity_id, state in HELPERS:
        if name in present:
            print(f"  already present: {name}")
            continue
        create(url, token, name, entity_id, state)
        print(f"  created: {name}  ({entity_id} == {state})")


if __name__ == "__main__":
    main()
