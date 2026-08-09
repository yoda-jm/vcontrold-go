# Contributing

The most valuable thing you can contribute is **evidence from a controller
other than the one this was written on**.

## The support table is honest, not complete

This project was developed against a single appliance: a Viessmann Vitola 200
VB2A with a Vitotronic 200 KW2 controller (`0x2098`). Everything else in the
support table in the README is transcribed from the community XML and has never
been run for real.

That is not modesty, it is a safety property. Two failure modes on this protocol
produce **plausible but wrong numbers instead of errors**:

- a length that is understated in `vito.xml`, which truncates a counter into
  something that still looks reasonable;
- a reserved "no data" value (`FF`, `FF FF`, `FF 7F`) published as if it were a
  measurement.

Both were found here on real hardware. Neither would have been caught by tests
alone, and neither looks like a bug in a graph until months later.

## Reporting a controller

Run the daemon with debug logging and send the first few seconds:

```bash
./vcontrold-go --yaml-config config-openv.yaml -log-level debug
```

The useful parts are the identification line and a handful of traced frames:

```
[...] INFO  Controller: V200KW2 ID=0x2098 (support: verified)
[...] DEBUG serial: read  0x0800 -> 01 F7 08 00 02
[...] DEBUG serial: read  0x0800 <- 13 01
```

Please say which readings you could **check against the controller's own
display**. A value confirmed on the front panel is worth more than ten that
merely look plausible.

## Adding an override

If a length or unit differs on your appliance, add it in `overrides/` with the
reading that proves it:

```xml
<!--
  Observed on <date> by reading the appliance directly.
  vito.xml declares <len>2</len>; read as 4 bytes the value is X, and the high
  bytes are <...>, so the counter has wrapped.
-->
<command name="getBrennerStarts" protocmd="getaddr">
  <addr>088A</addr>
  <len>4</len>
</command>
```

An override without evidence is a guess with extra steps.

## Code

```bash
make deps        # required: several tests skip without the community XML
make lint        # gofmt + go vet + staticcheck, all must pass
make test-race
```

Two conventions matter more than style:

1. **If a value exists in the community XML, do not hard-code it in Go.** The
   original write bug was exactly this: the `0xF4` opcode was in the XML while
   the Go code carried its own constant.
2. **Make a wrong value fail rather than publish.** An error is recoverable; a
   plausible wrong number in a year of history is not.

## Writes

`set*` commands act on a boiler someone is living with. The safe way to exercise
the write path changes nothing:

```bash
make verify-write ADDR=0x6300
```

It reads the value, writes the same bytes back, checks the status and reads
again. Please do not send a pull request touching the write path without having
run it.
