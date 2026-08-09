# vcontrold-go

Read and control a Viessmann heating controller over the Optolink serial
interface, and publish it to MQTT with Home Assistant auto-discovery.

This is a Go implementation of the KW2 protocol, alongside the C
[openv/vcontrold](https://github.com/openv/vcontrold) that documents it. It is
not a port: it reuses the community's XML — the protocol truth lives there — and
adds a single static binary, MQTT discovery that survives a Home Assistant
restart, and readings that refuse to lie about their own uncertainty.

---

## What is tested, and what is guessed

**This matters more than any feature list.** The project was written against one
boiler in one house. Everything below is labelled accordingly, and the daemon
tells you at startup which case you are in.

| Controller | ID | Status | What that means |
|---|---|---|---|
| **Vitotronic 200 KW2** | `0x2098` | **Verified** | Reads *and* writes exercised on real hardware (Vitola 200 VB2A). The counters, the setpoints, the write path — all confirmed against the appliance. |
| Vitotronic 200 KW1 | `0x2094` | Inferred | Same KW protocol and address list per the community XML. Never run here. |
| V200WB2 | `0x2053` | Inferred | The community XML routes it through GWG_VBEM, a different protocol. Reads are unlikely to work as-is. |
| VScotHO1 | `0x20CB` | Inferred | Listed as KW,300. This project speaks KW only. |
| anything else | — | Unknown | The KW protocol does not vary, so reads may work; no address list has been checked. |

"Inferred" means: transcribed from `vito.xml`, plausible, and completely
untested. If you run this against one of those, the readings are hypotheses
until you check a few by hand. Reports are welcome — that is how the table
grows.

At startup:

```
[2026-08-09 10:46:34] INFO  Controller: V200KW2 ID=0x2098 (support: verified)
```

### Where the guessing is dangerous

Two failure modes produce *plausible but wrong* numbers rather than errors, and
both were found on real hardware:

1. **`vito.xml` can understate a length.** It declares `getBrennerStarts`
   (`0x088A`) as two bytes; on this appliance the counter is four. Read short it
   reported 9707 instead of 206315 — the truncated low word, which looks like a
   perfectly reasonable number. See [`overrides/`](overrides/).

2. **A missing sensor is not an error.** The controller returns a reserved
   value: `FF`/`FF FF` for a datapoint the appliance does not have, `FF 7F`
   (`0x7FFF`) for a sensor that is not connected. Published as-is those became
   **6553.5 °C** and **3276.7 °C**. The daemon now recognises both and reports
   the datapoint as unavailable instead.

Both patterns are documented by the community's own reference profile,
`doc/examples/sim-2098.ini`, which lists exactly which datapoints a V200KW2 does
not carry.

---

## Features

- **KW2 protocol** over Optolink (4800 baud, 8N2, even parity), built from the
  macros in `vcontrold.xml` rather than hard-coded opcodes.
- **MQTT with Home Assistant discovery** — retained, so entities survive a
  restart, and carrying the `state_class` that keeps history alive.
- **Climate entities** for the setpoints the controller actually exposes.
- **Machine-specific overrides** applied on top of the community XML, never
  modifying it, so `make deps` stays safe.
- **Availability topic** with a last will, so a dead daemon shows as unavailable
  rather than as a stale reading.
- **Frame-level tracing** at debug level, for checking a value by hand.
- Single static binary, no runtime dependencies.

---

## Quick start

```bash
make deps                       # fetch the community XML into third_party/
cp config.example.yaml config-openv.yaml
$EDITOR config-openv.yaml       # serial device, MQTT broker, credentials
make build
./vcontrold-go --yaml-config config-openv.yaml
```

The serial device needs the `dialout` group:

```bash
sudo usermod -aG dialout "$USER"   # then log out and back in
```

> **`web: true` is mandatory for MQTT.** Publishing is carried by the web
> server; in CLI mode the daemon reads once and exits without publishing
> anything. This surprises everyone once.

### Reading a single address

```bash
./vcontrold-go --yaml-config config-openv.yaml -address 0x08A7
./vcontrold-go --yaml-config config-openv.yaml -address 0x088A -size 4
```

`-size` overrides the length from `vito.xml`, which is how the four-byte counter
above was discovered.

### Checking the write path safely

```bash
make verify-write ADDR=0x6300
```

Reads the setpoint, writes the same bytes back, checks the `00` status, reads
again. The value never changes, but it proves the SETADDR frame is accepted —
something no unit test can establish.

---

## Configuration

See [`config.example.yaml`](config.example.yaml); it is the deployed
configuration with credentials removed, and it is commented throughout.

### On the German identifiers

`getTempWWsoll`, `getBetriebArtM1`, `soll`, `ist`, `WW` — these are not our
names. They are the command names in the community `vito.xml`, and keeping them
is what lets any address here be traced back to the upstream XML and to the
openv documentation. Only the `friendly_name` fields are translated, because
those are what Home Assistant displays.

| Term | Meaning |
|---|---|
| `soll` | setpoint (target) |
| `ist` | actual (measured) |
| `WW` (Warmwasser) | domestic hot water |
| `Betriebsart` | operating mode |
| `Kessel` | boiler |
| `Vorlauf` | flow |
| `Brenner` | burner |
| `Neigung` / `Niveau` | heating curve slope / level |

### Address overrides

`vito.xml` describes a family of appliances, not one specimen. Overrides are
applied on top of it, so `make deps` can refresh the community XML without
losing what was measured locally:

```xml
<command name="getBrennerStarts" protocmd="getaddr">
  <addr>088A</addr>
  <len>4</len>          <!-- vito.xml says 2; this counter is really 4 bytes -->
  <unit>CO</unit>
</command>
```

Every override should cite the measurement that justifies it. A guessed length
is how the truncated counter happened in the first place.

---

## MQTT and Home Assistant

Values are published retained to `<topic_root>/<alias>`, and each monitored
address also gets a retained discovery message under `homeassistant/`.

**`state_class` is the field that matters.** Without it Home Assistant keeps the
entity out of long-term statistics and the history disappears with the recorder
purge, ten days by default. It is derived from the `vito.xml` unit —
`total_increasing` for counters, `measurement` for temperatures — and can be
overridden per address.

The broker account needs to write both trees, and Home Assistant needs to read
the state tree. With mosquitto:

```
user vcontrold
topic readwrite openv/#
topic write homeassistant/#

user homeassistant
topic read openv/#
```

> A mosquitto account that exists in `passwd` but has **no entry** in `aclfile`
> is denied everything, and in MQTT 3.1.1 the PUBACK is indistinguishable from
> success. Use `mosquitto_pub -V 5` to see the real reason code: `135` is Not
> authorized, `16` is accepted with no subscriber.

A ready-made dashboard view is in
[`config/home_assistant_vitola.yaml`](config/home_assistant_vitola.yaml).

---

## Development

```bash
make lint        # gofmt + go vet + staticcheck (pinned version)
make test        # unit tests
make test-race   # tests under the race detector
make cover       # coverage per package
make tools       # install the pinned lint tooling
```

`make deps` must run first: several tests read the community `vito.xml` and skip
without it, which would quietly hollow out the run.

The tests deliberately assert what fails *silently* on real hardware — a
discovery payload missing its `state_class`, a setpoint encoded on two bytes
instead of one, a reserved "no data" value published as a measurement. None of
those surface as a crash; they surface as wrong history weeks later.

### Debugging

```bash
./vcontrold-go --yaml-config config-openv.yaml -log-level debug
```

Every serial frame is traced, so a value can be verified by hand:

```
[2026-08-09 10:46:36] DEBUG serial: read  0x0800 -> 01 F7 08 00 02
[2026-08-09 10:46:36] DEBUG serial: read  0x0800 <- 13 01
[2026-08-09 10:46:36] DEBUG viessmann: Successfully read getTempA: 27.5 Grad Celsius
```

`13 01` little-endian is `0x0113` = 275, and unit `UT` is `V/10`, so 27.5 °C.

Levels: `debug`, `info` (default), `warn`, `error`. `-verbose` is shorthand for
`debug`.

---

## Deployment

`make deploy` sends the binary, configuration and overrides to the target host
named in `.env`; `deploy/vcontrold-go.service` is a systemd template.

For a user service, `loginctl enable-linger` is required to survive a reboot —
and note that the user manager keeps the groups of the session that started it,
so after adding yourself to `dialout` it must be restarted or the serial port
stays "Permission denied" while `id` says otherwise.

---

## Protocol notes

Read and write both come from the `<protocol name="KW2">` macros:

```xml
<macro name="SYNC">    <command>SEND 04;WAIT 05</command></macro>
<macro name="GETADDR"> <command>SEND 01 F7</command></macro>
<macro name="SETADDR"> <command>SEND 01 F4</command></macro>
```

Three things worth knowing, each of which cost a bug here:

- **KW carries no checksum.** That belongs to P300. A write is
  `01 F4 <addr_hi> <addr_lo> <len> <data…>` and nothing more.
- **A write answers with a status byte**, unit `SR`: `00` is OK, `05` is
  NOT OK — not the `05` acknowledgement that ends the SYNC.
- **The controller emits its `05` intermittently.** Bounding a read by a number
  of attempts fails on a healthy link; bound it by elapsed time. Back-to-back
  transactions also need a short rest, or parallel refresh groups desynchronise
  the controller.

---

## Contributing

Useful contributions, roughly in order of value:

1. **Reports from other controllers.** The support table above is thin because
   it is honest. `-log-level debug` output plus the startup identification line
   is enough to add a row.
2. **Overrides with evidence.** A length or unit that differs on your appliance,
   with the reading that shows it.
3. Anything that makes a wrong value *fail* rather than publish.

Keep the distinction between measured and assumed: a comment saying which one a
value is worth more than the value itself.

## License

See [LICENSE](LICENSE).

## Acknowledgments

[openv/vcontrold](https://github.com/openv/vcontrold) for the protocol
documentation and the XML that carries it, and
[philippoo66/optolink-splitter](https://github.com/philippoo66/optolink-splitter)
for the datapoint work.
