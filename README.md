# vcontrold-go

A modern Go-based monitoring and control system for Viessmann V200KW2 heating systems using the KW2 protocol.

## Features

- **KW2 Protocol Support**: Native implementation of the Viessmann KW2 communication protocol
- **Multi-Source Unit Detection**: Intelligent unit detection using three sources:
  - `vito.xml`: Exact address mappings from Viessmann configuration
  - Parameter name inference: Smart detection based on parameter names
  - Address pattern matching: Fallback detection using known address ranges
- **Advanced Decoding**: Proper decoding of complex Viessmann data formats:
  - **CycleTime**: Heating/cooling schedule decoding with exact bit manipulation
  - **ErrorState**: Error history with timestamps (BCD timestamp decoding)
  - **Temperature**: Multi-precision temperature readings (V/10, V/2, etc.)
  - **Enums**: Status codes, operating modes, error codes with human-readable text
- **Web Interface**: Modern Bootstrap 5 web interface with:
  - Real-time parameter monitoring
  - Auto-refresh functionality
  - XML status indicators
  - Categorized parameter display sorted by address
  - Success rate statistics
- **XML Integration**: Full support for vcontrold ecosystem XML files:
  - `vcontrold.xml`: Unit definitions and conversions
  - `vito.xml`: Address mappings and parameter definitions
- **MQTT Ready**: Placeholder implementation for Home Assistant integration
- **Extensible Architecture**: Clean, modular Go code structure

## Installation

### Prerequisites

- Go 1.23 or later
- Serial access to Viessmann V200KW2 heating system
- vcontrold XML configuration files

### Building

```bash
git clone https://github.com/yourusername/vcontrold-go.git
cd vcontrold-go

# Build using Makefile (recommended)
make build

# Or build directly with go
go build ./cmd/vcontrold-go
```

## Usage

### Command Line Options

```bash
./vcontrold-go [options]
```

**Options:**
- `-device string`: Serial device to use (default "/dev/ttyUSB0")
- `-config string`: V200KW2 configuration file to parse (optional)
- `-vcontrol-xml string`: Path to vcontrold.xml unit definitions (default "xml/vcontrold.xml")
- `-vito-xml string`: Path to vito.xml address mappings (default "xml/vito.xml")
- `-port string`: HTTP server port (default "8080")
- `-web`: Start web server instead of CLI
- `-verbose`: Show verbose output

### Web Interface Mode

Start the web server:

```bash
# Using the built binary
./build/vcontrold-go -web -config example_config.txt

# Or using make for development
make dev
```

Then open your browser to `http://localhost:8080`

### CLI Mode

Read all parameters once:

```bash
./build/vcontrold-go -config example_config.txt
```

### Configuration File Format

The configuration file defines which parameters to read:

```
# Parameter configuration file for V200KW2
# Format: name address size datatype [additional_fields]

"Kesseltemperatur" 0x0802 2 SInt
"Aussentemperatur" 0x5525 2 SInt  
"Betriebsart HK1" 0x2301 1 Byte
"Warmwassertemperatur" 0x0804 2 SInt
"Heizkreispumpe HK1" 0x2906 1 Byte
```

**Fields:**
- `name`: Human-readable parameter name (quoted if contains spaces)
- `address`: Memory address in hex format (0xXXXX)
- `size`: Number of bytes to read
- `datatype`: Data type (`Byte`, `SInt`, `Int`, `Int4`, `Array`)

## XML Configuration

### vcontrold.xml

Defines unit conversions and enums:

```xml
<units>
  <unit name="Temperatur">
    <calc get="V/10"/>
    <entity>°C</entity>
  </unit>
  
  <unit name="BetriebsArt">
    <type>enum</type>
    <enum bytes="00" text="ABSCHALT"/>
    <enum bytes="01" text="HEIZEN"/>
    <enum bytes="02" text="REDUZ"/>
  </unit>
</units>
```

### vito.xml

Maps addresses to unit types:

```xml
<commands>
  <command name="getKesseltemp" protocmd="getaddr">
    <addr>0802</addr>
    <len>2</len>
    <unit>UT</unit>
    <description>Kesseltemperatur</description>
  </command>
</commands>
```

## Supported Data Types

### Temperature Readings
- **Standard Temperature** (`UT`): Signed 16-bit, divided by 10 (±3276.7°C range)
- **1-Byte Temperature** (`UT1`): Unsigned 8-bit, divided by 2 (±127.5°C range) 
- **Integer Temperature** (`UTI`): Raw integer temperature values

### Status and Control
- **Operating Modes** (`BA`): Enum values (OFF, HEATING, REDUCED, etc.)
- **Status Fields** (`ST`, `RT`): Various status enums
- **Error States** (`ES`): Error codes with descriptive text

### Specialized Formats
- **CycleTime** (`CT`): Heating schedules with ON/OFF times
- **Error History**: 9-byte format with error code + BCD timestamp
- **System Time** (`TI`): BCD-encoded date/time values
- **Counters**: Operating hours, start counts, etc.

## Error History Decoding

The system automatically detects and decodes error history entries:

```
Error History Entry 1: 2021-06-06T17:16:48 Brennerstoerung
Error History Entry 2: 2021-06-05T14:23:15 Kommunikationsfehler
```

Each entry contains:
- BCD timestamp (8 bytes)
- Error code (1 byte) 
- Human-readable error description from XML

## CycleTime Decoding

Heating schedules are decoded using exact Viessmann bit manipulation:

```
1:An:06:30  Aus:22:00  2:An:--     Aus:--  3:An:07:00  Aus:23:30
```

Format: `Slot:On:HH:MM  Off:HH:MM`

## Web Interface

The web interface provides:

- **Real-time Monitoring**: Live parameter values with auto-refresh
- **XML Status Indicators**: Visual indicators showing which parameters have XML definitions
- **Categorized Display**: Parameters grouped by type (Temperature, Operating Mode, etc.)
- **Address Sorting**: Parameters sorted by memory address within each category
- **Success Statistics**: Real-time success rates and error counts
- **Mobile Responsive**: Bootstrap 5 responsive design

### XML Status Icons
- ✓ **Green**: Parameter found in XML with proper unit conversion
- ? **Red**: Parameter not in XML, using inference or fallback

## MQTT Integration (Planned)

The system includes MQTT client placeholder for future Home Assistant integration:

```bash
# Planned MQTT support
./vcontrold-go -web -mqtt-broker homeassistant.local -mqtt-topic viessmann
```

Features will include:
- Home Assistant discovery
- Real-time parameter publishing
- Command reception for parameter changes
- Scheduled reading support

## Architecture

```
cmd/
├── vcontrold-go/          # Main application entry point
internal/
├── config/                # Configuration parsing
├── viessmann/             # KW2 protocol and reading logic
├── xml/                   # XML parsing (vcontrold.xml, vito.xml)
├── web/                   # HTTP server and web interface
└── mqtt/                  # MQTT client (placeholder)
```

## Protocol Details

### KW2 Communication
The KW2 protocol is a proprietary Viessmann protocol using:
- **Baud Rate**: 4800
- **Data Bits**: 8
- **Parity**: Even  
- **Stop Bits**: 2

### Communication Sequence
1. **Initialize**: Send `0x04`, expect `0x06`
2. **Read Command**: `0x01 0xF7 <addr_low> <addr_high> <length>`
3. **Response**: `<status> <data...> <checksum>`

## Development

### Project Structure
- `cmd/`: Application entry points
- `internal/`: Internal packages (not importable by other projects)
- `pkg/`: Public packages (if any)

### Adding New Unit Types
1. Add unit definition to `vcontrold.xml`
2. Add address mapping to `vito.xml` 
3. Update `ConvertVitoUnitToXMLUnit()` in `internal/xml/vito.go`
4. Add decoding logic if needed in `internal/viessmann/decoder.go`

### Testing Serial Communication

```bash
# Test with verbose output
./vcontrold-go -device /dev/ttyUSB0 -config test.txt -verbose

# Test web interface
./vcontrold-go -web -device /dev/ttyUSB0 -config test.txt
```

## Hardware Setup

### Serial Connection
- **Interface**: RS232 or USB-to-Serial adapter
- **Connector**: Viessmann Optolink interface
- **Wiring**: Connect to heating system's optical interface

### Optolink Cable
The Viessmann Optolink uses an optical interface:
- IR LED for TX
- Phototransistor for RX  
- 5V power supply

## Troubleshooting

### Common Issues

**Serial Port Access**:
```bash
# Add user to dialout group
sudo usermod -a -G dialout $USER
# Logout and login again

# Check device permissions
ls -l /dev/ttyUSB0
```

**Communication Errors**:
- Verify baud rate (4800)
- Check cable connections
- Ensure heating system is accessible
- Try different USB ports

**XML Loading Issues**:
- Verify XML file paths
- Check XML syntax
- Ensure files are readable

**Web Interface Issues**:
- Check port availability (default 8080)
- Verify firewall settings
- Check console for JavaScript errors

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Viessmann for the V200KW2 heating system
- The vcontrold project for XML configuration format inspiration
- The Go community for excellent libraries and tools

## Supported Systems

### Tested Hardware
- **Viessmann V200KW2**: Primary target system
- **Serial Adapters**: FTDI-based USB-to-Serial converters
- **Platforms**: Linux (primary), should work on Windows/macOS

### Compatibility
The system should work with other Viessmann systems using KW2 protocol, but parameter addresses and formats may vary. XML configuration files would need to be adapted for other models.

## Performance

- **Reading Speed**: ~2-3 seconds per parameter (limited by KW2 protocol)
- **Memory Usage**: <10MB typical usage
- **CPU Usage**: Minimal when idle, low during reading
- **Concurrent Access**: Serial access is mutex-protected

## Security Considerations

- **Serial Access**: Requires appropriate system permissions
- **Web Interface**: No authentication (add reverse proxy if needed)
- **MQTT**: Will support authentication when implemented
- **Local Access**: Designed for trusted local networks

---

## État du projet (8 août 2026)

Revue de code effectuée par comparaison avec l'implémentation de référence en C
(`openv/vcontrold`). **Voir [PLAN.md](PLAN.md) pour le plan d'action détaillé.**

| | |
|---|---|
| Lecture KW2 | conforme à la référence C, octet pour octet |
| Écriture KW2 | **cassée** — mauvais opcode, checksum parasite, mauvais ack |
| Conversions d'unités | correctes (`V/3600`, enums), mais dupliquées en dur |
| Publication MQTT | 3 adresses seulement, sans `state_class` |

## Démarrage rapide

```bash
cp .env.example .env                      # renseigner MQTT + machine cible
cp config.example.yaml config-openv.yaml  # adapter device et adresses
make deps                                 # XML communautaires -> third_party/
make build && make test
```

**Aucun secret dans ce dépôt.** `.env`, `config-openv.yaml` et `third_party/`
sont gitignorés. Voir [CLAUDE.md](CLAUDE.md) pour les conventions de travail.

### Matériel et déploiement

Chaudière **Viessmann Vitola 200 VB2A**, régulation **Vitotronic 200 KW2**,
interface optique **Optolink** en USB.

On développe sur le poste de travail, on exécute sur la machine où la sonde est
branchée :

```bash
make deploy       # rsync binaire + config + XML
make run-remote
make probe        # verifie la detection de l'adaptateur
```

### Dépendances externes

`scripts/bootstrap.sh` récupère dans `third_party/`, à des révisions épinglées :

- **`openv/vcontrold`** — `xml/kw/vcontrold.xml` (unités, macros de protocole)
  et `xml/kw/vito.xml` (adresses, longueurs). **Ces fichiers font autorité sur
  le protocole** : ne rien coder en dur qui s'y trouve déjà.
- **`philippoo66/optolink-splitter`** — liste de datapoints.
  *`DP_Listen_2.zip` n'est pas versionné en amont — voir PLAN.md, tâche 7.*

### Piège connu

Le port série n'est pas garanti sur `/dev/ttyUSB1` d'un branchement à l'autre.
Préférer `ls -l /dev/serial/by-id/`, et vérifier l'appartenance au groupe
`dialout` — cause numéro un des « permission denied ».
