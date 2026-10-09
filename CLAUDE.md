# Working on this repository

This file describes **how work is done here**. It is loaded automatically by
Claude Code, but it is written for anyone.

## The physical setup

Two machines, and that shapes everything:

| | Role |
|---|---|
| **Development machine** | where code is written, compiled and unit-tested |
| **Target machine** | the Optolink USB adapter is plugged in here; the only one that talks to the boiler |

**Never build on the target.** Build here, ship, run there:

```bash
make build        # cross-compile for the target (TARGET_GOARCH in .env)
make deploy       # rsync binary + config + overrides
make run-remote   # run on the target
make probe        # check the adapter is detected
```

Parameters come from `.env` (see `.env.example`) and can be overridden:
`make deploy TARGET_HOST=user@host`.

## What is not in the repository

**No secrets, no internal addresses.** Three paths are gitignored and must be
created from their templates:

| Real file | Template |
|---|---|
| `.env` | `.env.example` |
| `config-openv.yaml` | `config.example.yaml` |
| `third_party/` | `make deps` |

Before committing, check nothing leaks:

```bash
git diff --cached | grep -niE "password|token|192\.168\.|10\.[0-9]+\." && echo "STOP"
```

## The community XML is authoritative

`third_party/vcontrold/xml/kw/` holds `vcontrold.xml` (units, protocol macros)
and `vito.xml` (addresses, lengths). **Those files carry the protocol truth.**

Rule: **if a value exists in the XML, do not hard-code it in Go.** That is
exactly where the original write bug came from — the `0xF4` opcode was in the
XML all along, while the Go code carried its own constant.

They are pinned to a specific revision in `scripts/bootstrap.sh`. Moving that
revision is a deliberate act, never a side effect.

When the XML is wrong for a specific appliance, correct it in `overrides/` —
never by editing the community file, which `make deps` would overwrite.

## Say what is measured and what is assumed

This is the most important convention here, because the failures that hurt are
the ones that produce *plausible* numbers rather than errors.

- A value read from real hardware: say so, and give the reading.
- A value derived from the XML for an appliance nobody has tested: say that too.
- An override must cite the measurement that justifies it.

`internal/viessmann/device.go` encodes this as support levels. Only the
V200KW2 (`0x2098`) is `verified`; everything else is `inferred` or `unknown`.

## Verify on hardware, not only in tests

Unit tests prove nothing about the serial protocol. Any change touching
`internal/viessmann/serial_communicator.go` must be confirmed by a real read:

```bash
./vcontrold-go --yaml-config config-openv.yaml -address 0x08A7   # burner hours
./vcontrold-go --yaml-config config-openv.yaml -address 0x088A   # burner starts
```

**Consistency check**: starts must exceed `196980` (reading of 7 October 2025),
and hours must be consistent with that count — tens of thousands. A value near
`15` signals a broken conversion.

The daemon holds the serial port, so stop it first:
`systemctl --user stop vcontrold-go`.

## Care with writes

`set*` commands act on a boiler in service, in a house someone lives in.
**Never write blind.**

Protocol: read the value, write it back unchanged, check the `00` status, read
again. Only then change anything. `make verify-write ADDR=0x6300` does exactly
that.

## Style

- Code, comments and documentation in **English**. The only exception is the
  strings Home Assistant displays (`friendly_name`, dashboard labels), which
  stay French because that interface is French.
- The German protocol identifiers (`soll`, `ist`, `WW`, `Betriebsart`) are kept
  as-is: they are the community XML's command names, and renaming them would
  break the link to upstream documentation.
- Commit messages in English, imperative mood, explaining **why** when the
  what is not enough.
- No compiled binary in the repository (`make clean` before committing).
- `make lint` must pass: gofmt, go vet and staticcheck.
