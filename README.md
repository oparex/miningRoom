# Mining Room Dashboard

A mining rig monitoring dashboard built with Go/Gin backend and Bootstrap 5 frontend using server-side rendering.

## Features

- Real-time status monitoring for mining rigs
- Power consumption and hashrate gauges
- Historical charts for performance tracking
- Individual and bulk miner control (power settings, start/shutdown)
- SQLite-based configuration with settings page for miner management

## Requirements

- Go 1.21+
- QuestDB (for metrics storage)

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

Machines are defined in `config.yaml`:

```yaml
machines:
  - name: "Rig-01"
    ip: "192.168.1.101"
  - name: "Rig-02"
    ip: "192.168.1.102"
```

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
└── static/
    ├── css/             # Custom styles
    └── js/              # Chart.js and dashboard logic
```
