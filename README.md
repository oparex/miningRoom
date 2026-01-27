# Mining Room Dashboard

A mining rig monitoring dashboard built with Go/Gin backend and Bootstrap 5 frontend using server-side rendering.

## Features

- Real-time status monitoring for mining rigs
- Power consumption and hashrate gauges
- Historical charts for performance tracking
- Individual and bulk miner control (power settings, start/shutdown)
- Environment monitoring (temperature, humidity, pressure)
- SQLite-based configuration with settings page for miner management

## Requirements

- Go 1.21+
- QuestDB (metrics storage)
- Mosquitto (MQTT broker)
- Telegraf (metrics collection)

## Installation

```bash
# Clone the repository
git clone <repository-url>
cd miningRoom

# Install dependencies
go mod tidy

# Build the application
go build -o dashboard .
```

## Configuration

Miner configurations are stored in SQLite and managed through the settings page in the dashboard UI.

## Data Collection Stack

```
Miners / Sensors → Mosquitto (MQTT) → Telegraf → QuestDB → Dashboard
```

### Components

- **Mosquitto** - MQTT broker that receives data from miners and environment sensors
- **Telegraf** - Collects metrics from MQTT and writes to QuestDB
- **QuestDB** - Time-series database for storing historical metrics

### Telegraf Configuration

Check the config file in `telegraf/` directory.

## Environment Sensors

The dashboard integrates with ESP8266-based environment sensors running [esp8266-bme280](https://github.com/oparex/esp8266-bme280).

These sensors use a BME280 module to measure:
- Temperature (°C)
- Relative humidity (%)
- Atmospheric pressure (hPa)

Data is published to the Mosquitto MQTT broker in JSON format, then collected by Telegraf and stored in QuestDB for visualization in the dashboard.

## Usage

```bash
# Run the application (serves on port 8080)
go run main.go

# Or run the built binary
./dashboard
```

Then open http://localhost:8080 in your browser.

## API Endpoints

### Dashboard Data
- `GET /api/status` - System status
- `GET /api/gauges` - Gauge values
- `GET /api/charts` - Chart data

### Miner Control (Individual)
- `POST /api/miner/power` - Set power `{ip, power}`
- `POST /api/miner/start` - Start miner `{ip}`
- `POST /api/miner/shutdown` - Shutdown miner `{ip}`

### Miner Control (Bulk)
- `POST /api/miners/power` - Set power `{ips[], power}`
- `POST /api/miners/start` - Start miners `{ips[]}`
- `POST /api/miners/shutdown` - Shutdown miners `{ips[]}`

## Project Structure

```
.
├── main.go              # Gin web server with routes and handlers
├── config/
│   └── config.go        # Configuration loader
├── templates/           # Go HTML templates
│   ├── dashboard.html   # Main overview page
│   └── manage.html      # Miner control page
├── static/
│   ├── css/             # Custom styles
│   └── js/              # Chart.js and dashboard logic
└── telegraf/            # Telegraf configuration files
```
