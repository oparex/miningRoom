# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Run Commands

```bash
# Build the application
go build -o dashboard .

# Run the application (serves on port 8080)
go run main.go

# Install dependencies
go mod tidy
```

## Architecture

This is a mining rig monitoring dashboard built with Go/Gin backend and Bootstrap 5 frontend using server-side rendering.

### Backend Structure

- `main.go` - Gin web server with routes and handlers
- `config/config.go` - YAML configuration loader for mining machines
- `config.yaml` - Machine definitions (name, IP address)

### Frontend (Server-Side Rendered)

- `templates/` - Go HTML templates rendered by Gin
  - `dashboard.html` - Main overview page with status, gauges, and charts
  - `manage.html` - Miner control page with power settings and start/shutdown
- `static/css/` - Custom styles
- `static/js/` - Chart.js initialization and dashboard logic

### API Endpoints

**Dashboard Data:**
- `GET /api/status` - System status
- `GET /api/gauges` - Gauge values
- `GET /api/charts` - Chart data

**Miner Control (individual):**
- `POST /api/miner/power` - Set power `{ip, power}`
- `POST /api/miner/start` - Start miner `{ip}`
- `POST /api/miner/shutdown` - Shutdown miner `{ip}`

**Miner Control (bulk - selected miners):**
- `POST /api/miners/power` - Set power `{ips[], power}`
- `POST /api/miners/start` - Start miners `{ips[]}`
- `POST /api/miners/shutdown` - Shutdown miners `{ips[]}`

### Configuration

Machines are defined in `config.yaml`:
```yaml
machines:
  - name: "Rig-01"
    ip: "192.168.1.101"
```

The config is loaded at startup and available globally via `cfg` variable.
