# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Run Commands

```bash
# Install dependencies
go mod tidy

# Build the application
go build -o dashboard .

# Run the application (serves on port 8080)
go run main.go

# Run with custom flags
go run main.go --db-path miningroom.db --questdb-host localhost --questdb-port 9001 --miner-user root --miner-pass root
```

There are no tests in this project.

## Architecture

Mining rig monitoring dashboard built with Go/Gin backend and Bootstrap 5 frontend using server-side rendering. Metrics flow from miners and sensors through MQTT/Telegraf into QuestDB, then are queried and rendered by the Go backend.

```
Miners/Sensors → MQTT (Mosquitto) → Telegraf → QuestDB (time-series)
                                                      ↓
                              Browser ← Go/Gin backend ← SQLite (machine config)
```

### Backend Structure

- `main.go` - Gin web server (~1500 lines): routes, handlers, HTTP digest auth, miner control via kaonsu API, Shelly relay control, BTC revenue calculation
- `db/db.go` - SQLite database layer: `Machine` struct (Name, IP, ShellyIP), CRUD operations, schema migration
- `questdb/client.go` - QuestDB HTTP client (~1000 lines): time-series queries for hashrate, temperatures, power, environment data, thermal insulation, daily energy

### Frontend (Server-Side Rendered)

- `templates/` - Go HTML templates rendered by Gin
  - `dashboard.html` - Main overview with status, gauges, and charts
  - `miners.html` - Miner metrics and temperature data
  - `power-mining.html` - Power consumption and mining revenue monitoring
  - `environment.html` - Environment sensor data (temperature, humidity, pressure)
  - `manage.html` - Miner control (power settings, start/shutdown)
  - `settings.html` - Machine management (add/remove miners, configure Shelly IPs)
- `static/css/dashboard.css` - Custom styles with CSS variables for light/dark theme
- `static/js/dashboard.js` - Sidebar toggle, real-time clock, async data loading, 60s auto-refresh
- `static/js/theme.js` - Dark/light mode toggle with localStorage persistence

### Configuration

Machines are stored in SQLite (default: `miningroom.db`), managed via the Settings page or API. No config file is used.

CLI flags:
- `--db-path` (default: `miningroom.db`) - SQLite database path
- `--questdb-host` (default: `localhost`) - QuestDB host
- `--questdb-port` (default: `9001`) - QuestDB HTTP port
- `--miner-user` (default: `root`) - Miner HTTP digest auth username
- `--miner-pass` (default: `root`) - Miner HTTP digest auth password

### Telegraf

`telegraf/telegraf.conf` configures metric collection:
- MQTT inputs for Shelly devices (power, current, voltage) and BME280 sensors (temperature, humidity, pressure)
- HTTP inputs polling 5 mining rigs for hashboard and pool data
- Output to QuestDB via InfluxDB line protocol (port 9000)

## API Endpoints

**Pages (GET, return HTML):**
- `/` - Dashboard
- `/miners` - Miner metrics
- `/power-mining` - Power and mining
- `/environment` - Environment sensors
- `/manage` - Miner control
- `/settings` - Machine management

**Dashboard Data (GET, return JSON):**
- `/api/status` - System status
- `/api/gauges` - Gauge values
- `/api/charts` - Chart data
- `/api/charts/environment` - Environment temperature charts
- `/api/charts/miner-temperatures` - Miner temperature charts
- `/api/charts/humidity` - Humidity charts
- `/api/charts/pressure` - Pressure charts
- `/api/charts/hourly-temp` - Hourly temperature chart
- `/api/charts/thermal-insulation` - Thermal insulation coefficient (W/K)
- `/api/charts/daily-energy` - Daily energy usage (kWh)
- `/api/miners/status` - Miner status table data
- `/api/environment/latest` - Latest environment readings
- `/api/manage/miners` - Miner config for management page

**Miner Control (POST, individual):**
- `/api/miner/power` - Set power target `{ip, power}`
- `/api/miner/start` - Start miner `{ip}`
- `/api/miner/shutdown` - Shutdown miner `{ip}`

**Miner Control (POST, bulk):**
- `/api/miners/power` - Set power `{ips[], power}`
- `/api/miners/freq` - Set frequency/voltage `{ips[], freq, volt}`
- `/api/miners/sleep` - Set sleep mode `{ips[], ...}`
- `/api/miners/start` - Start miners `{ips[]}`
- `/api/miners/shutdown` - Shutdown miners `{ips[]}`

**Machine Management:**
- `POST /api/machines` - Add machine `{name, ip, shelly_ip}`
- `DELETE /api/machines/:ip` - Delete machine by IP

## Key Patterns

- **Error handling**: Explicit error returns, logged with `log.Printf`
- **Concurrency**: `sync.WaitGroup` for parallel miner HTTP requests
- **Auth**: HTTP Digest Authentication (MD5) for miner API calls
- **External APIs**: mempool.space for BTC price and network hashrate; Shelly Gen2 RPC API for relay control
- **Global state**: `machines`, `database`, `questdbClient`, `minerUser`, `minerPass` are package-level variables in main.go
- **Naming**: Go standard (PascalCase exported, camelCase unexported)
- **Dependencies**: Only two direct deps: `gin-gonic/gin` and `mattn/go-sqlite3`
