#!/usr/bin/env python3
"""Mirror each thermostatic valve's setpoint as a numeric sensor.

Why
---
Home Assistant keeps **no long-term statistics for `climate` entities**. A
valve's target temperature lives in the `states` table and is purged after
`purge_keep_days`, so past setpoints are unrecoverable beyond ten days.

That is why the negative-slope finding — rooms drifting warmer as it gets colder
outside — had to be tested indirectly, by comparing night and day regressions
rather than by simply reading what the valves were asking for.

A `sensor` carrying `state_class: measurement` is archived indefinitely. Creating
these before the heating season is what makes next winter's version of the same
question answerable directly.

The setpoints read `unknown` while the valves are off. That is expected and
harmless: the series starts accumulating the moment heating resumes.

Names are final. Renaming a helper renames its entity, which orphans the
statistics keyed on that entity_id and starts an empty series.

    export HA_URL=http://homeassistant.local:8123
    export HA_TOKEN=<long-lived access token>
"""

import json
import os
import sys
import urllib.error
import urllib.request

# room label -> climate entity. The label becomes the entity id, so it is fixed.
VALVES = {
    "bureau": "climate.thermostat_bureau_thermostat",
    "chambre": "climate.thermostat_chambre_thermostat",
    "chambre ami": "climate.thermostat_chambre_ami_thermostat",
    "couloir": "climate.thermostat_couloir_thermostat",
    "cuisine": "climate.thermostat_cuisine_thermostat",
    "cuisine petit": "climate.thermostat_cuisine_petit_thermostat",
    "retro gaming": "climate.thermostat_retro_gaming_thermostat",
    "salle a manger": "climate.thermostat_salle_a_manger_thermostat",
    "salon": "climate.thermostat_salon_thermostat",
    "studio": "climate.thermostat_studio_thermostat",
}


def request(url, token, path, payload=None, method=None):
    req = urllib.request.Request(
        url + path,
        data=json.dumps(payload).encode() if payload is not None else None,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        method=method or ("POST" if payload is not None else "GET"),
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            return json.loads(response.read())
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"{path}: {exc.code} {exc.read().decode()[:200]}") from exc


def existing_titles(url, token):
    entries = request(url, token, "/api/config/config_entries/entry")
    return {e["title"] for e in entries if e.get("domain") == "template"}


def create(url, token, name, climate_entity):
    flow = request(url, token, "/api/config/config_entries/flow",
                   {"handler": "template", "show_advanced_options": True})
    path = f"/api/config/config_entries/flow/{flow['flow_id']}"

    request(url, token, path, {"next_step_id": "sensor"})

    # A valve that is off reports no target temperature. Emitting an empty
    # string leaves the sensor unknown for that period, which is the honest
    # representation: no setpoint was being asked for.
    state = ("{{ state_attr('" + climate_entity + "', 'temperature') "
             "if state_attr('" + climate_entity + "', 'temperature') is not none else none }}")

    step = request(url, token, path, {
        "name": name,
        "state": state,
        "unit_of_measurement": "°C",
        "device_class": "temperature",
        "state_class": "measurement",
    })

    if step.get("type") != "create_entry":
        raise RuntimeError(f"{name}: flow ended as {step.get('type')} "
                           f"step={step.get('step_id')} errors={step.get('errors')}")
    return step


def main():
    url, token = os.environ.get("HA_URL"), os.environ.get("HA_TOKEN")
    if not url or not token:
        sys.exit("set HA_URL and HA_TOKEN in the environment")

    present = existing_titles(url, token)
    for room, entity in VALVES.items():
        name = f"Consigne vanne {room}"
        if name in present:
            print(f"  already present: {name}")
            continue
        create(url, token, name, entity)
        print(f"  created: {name}  <- {entity}")


if __name__ == "__main__":
    main()
