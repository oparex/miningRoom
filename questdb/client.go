package questdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type QueryResult struct {
	Query   string          `json:"query"`
	Columns []Column        `json:"columns"`
	Dataset [][]interface{} `json:"dataset"`
	Count   int             `json:"count"`
}

// TotalHashrateResult represents the parsed result of the total hashrate query
type TotalHashrateResult struct {
	Timestamp     string  // ISO 8601 timestamp of the latest data
	TotalHashrate float64 // Sum of hashrate_average across all miners/pools
	HasData       bool    // Whether any data was returned
}

// MaxTemperatureResult represents the parsed result of the max temperature query
type MaxTemperatureResult struct {
	Timestamp      string  // ISO 8601 timestamp of the latest data
	MaxTemperature float64 // Maximum temperature across all hashboards
	HasData        bool    // Whether any data was returned
}

// AvgTemperatureResult represents the parsed result of the average max temperature query
type AvgTemperatureResult struct {
	AvgTemperature float64
	HasData        bool
}

// TotalPowerResult represents the parsed result of the total power query
type TotalPowerResult struct {
	Timestamp  string  // ISO 8601 timestamp of the latest data
	TotalPower float64 // Sum of power across all Shelly devices
	HasData    bool    // Whether any data was returned
}

// RoomTemperatureResult represents the parsed result of the room temperature query
type RoomTemperatureResult struct {
	Timestamp   string  // ISO 8601 timestamp of the reading
	Temperature float64 // Room temperature from BME280 sensor
	HasData     bool    // Whether any data was returned
}

func NewClient(host string, port int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Query(query string) (*QueryResult, error) {
	endpoint := fmt.Sprintf("%s/exec", c.baseURL)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := url.Values{}
	q.Set("query", query)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result QueryResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// GetTotalHashrate queries QuestDB for the latest total hashrate across all miners
// It uses a LATEST ON query to get the most recent reading from each miner/pool combination
// and sums them together to get the total hashrate.
func (c *Client) GetTotalHashrate() (*TotalHashrateResult, error) {
	const query = "SELECT timestamp, sum(hashrate_average) FROM pools LATEST ON timestamp PARTITION BY miner_ip, idx;"

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query total hashrate: %w", err)
	}

	// Check if we have any data
	if result.Count == 0 || len(result.Dataset) == 0 {
		return &TotalHashrateResult{
			HasData: false,
		}, nil
	}

	// Parse the first row: [timestamp, sum(hashrate_average)]
	row := result.Dataset[0]
	if len(row) < 2 {
		return nil, fmt.Errorf("unexpected result format: expected 2 columns, got %d", len(row))
	}

	// Parse timestamp (string)
	timestamp, ok := row[0].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected timestamp type: %T", row[0])
	}

	// Parse hashrate (float64)
	var hashrate float64
	switch v := row[1].(type) {
	case float64:
		hashrate = v
	case int:
		hashrate = float64(v)
	case int64:
		hashrate = float64(v)
	default:
		return nil, fmt.Errorf("unexpected hashrate type: %T", row[1])
	}

	return &TotalHashrateResult{
		Timestamp:     timestamp,
		TotalHashrate: hashrate,
		HasData:       true,
	}, nil
}

// GetMaxTemperature queries QuestDB for the maximum temperature across all hashboards.
// It uses a LATEST ON query to get the most recent reading from each miner/hashboard,
// takes the higher of the two temperature sensors, and returns the max.
func (c *Client) GetMaxTemperature() (*MaxTemperatureResult, error) {
	const query = `SELECT timestamp, max(CASE WHEN temperature_raw_1>=temperature_raw_0 THEN temperature_raw_1 ELSE temperature_raw_0 END) AS max_temp FROM hashboards LATEST ON timestamp PARTITION BY miner_ip, idx;`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query max temperature: %w", err)
	}

	// Check if we have any data
	if result.Count == 0 || len(result.Dataset) == 0 {
		return &MaxTemperatureResult{
			HasData: false,
		}, nil
	}

	// Parse the first row: [timestamp, max_temp]
	row := result.Dataset[0]
	if len(row) < 2 {
		return nil, fmt.Errorf("unexpected result format: expected 2 columns, got %d", len(row))
	}

	// Parse timestamp (string)
	timestamp, ok := row[0].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected timestamp type: %T", row[0])
	}

	// Parse temperature (float64 or int)
	var temperature float64
	switch v := row[1].(type) {
	case float64:
		temperature = v
	case int:
		temperature = float64(v)
	case int64:
		temperature = float64(v)
	default:
		return nil, fmt.Errorf("unexpected temperature type: %T", row[1])
	}

	return &MaxTemperatureResult{
		Timestamp:      timestamp,
		MaxTemperature: temperature,
		HasData:        true,
	}, nil
}

// GetAvgMaxTemperature queries QuestDB for the average of per-miner max temperatures.
// For each miner it takes the max of the two temperature sensors across all hashboards,
// then averages those maxes across all miners.
func (c *Client) GetAvgMaxTemperature() (*AvgTemperatureResult, error) {
	const query = `SELECT avg(max_temp) FROM (SELECT miner_ip, max(CASE WHEN temperature_raw_1>=temperature_raw_0 THEN temperature_raw_1 ELSE temperature_raw_0 END) AS max_temp FROM hashboards LATEST ON timestamp PARTITION BY miner_ip, idx);`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query avg max temperature: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 || len(result.Dataset[0]) == 0 {
		return &AvgTemperatureResult{HasData: false}, nil
	}

	return &AvgTemperatureResult{
		AvgTemperature: parseFloat(result.Dataset[0][0]),
		HasData:        true,
	}, nil
}

// GetTotalPower queries QuestDB for the total power consumption across all Shelly devices.
// It uses a LATEST ON query to get the most recent reading from each device and sums them.
func (c *Client) GetTotalPower() (*TotalPowerResult, error) {
	const query = "SELECT timestamp, sum(power) FROM shellies LATEST ON timestamp PARTITION BY device_id;"

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query total power: %w", err)
	}

	// Check if we have any data
	if result.Count == 0 || len(result.Dataset) == 0 {
		return &TotalPowerResult{
			HasData: false,
		}, nil
	}

	// Parse the first row: [timestamp, sum(power)]
	row := result.Dataset[0]
	if len(row) < 2 {
		return nil, fmt.Errorf("unexpected result format: expected 2 columns, got %d", len(row))
	}

	// Parse timestamp (string)
	timestamp, ok := row[0].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected timestamp type: %T", row[0])
	}

	// Parse power (float64 or int)
	var power float64
	switch v := row[1].(type) {
	case float64:
		power = v
	case int:
		power = float64(v)
	case int64:
		power = float64(v)
	default:
		return nil, fmt.Errorf("unexpected power type: %T", row[1])
	}

	return &TotalPowerResult{
		Timestamp:  timestamp,
		TotalPower: power,
		HasData:    true,
	}, nil
}

// GetRoomTemperature queries QuestDB for the room temperature from the BME280 sensor.
func (c *Client) GetRoomTemperature() (*RoomTemperatureResult, error) {
	const query = "SELECT timestamp, temperature FROM bme280_readings WHERE location='miningroom' ORDER BY timestamp DESC LIMIT 1;"

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query room temperature: %w", err)
	}

	// Check if we have any data
	if result.Count == 0 || len(result.Dataset) == 0 {
		return &RoomTemperatureResult{
			HasData: false,
		}, nil
	}

	// Parse the first row: [timestamp, temperature]
	row := result.Dataset[0]
	if len(row) < 2 {
		return nil, fmt.Errorf("unexpected result format: expected 2 columns, got %d", len(row))
	}

	// Parse timestamp (string)
	timestamp, ok := row[0].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected timestamp type: %T", row[0])
	}

	// Parse temperature (float64 or int)
	var temperature float64
	switch v := row[1].(type) {
	case float64:
		temperature = v
	case int:
		temperature = float64(v)
	case int64:
		temperature = float64(v)
	default:
		return nil, fmt.Errorf("unexpected temperature type: %T", row[1])
	}

	return &RoomTemperatureResult{
		Timestamp:   timestamp,
		Temperature: temperature,
		HasData:     true,
	}, nil
}

// MinerStatusRow represents the latest status of a single miner
type MinerStatusRow struct {
	Timestamp      string  `json:"timestamp"`
	MinerIP        string  `json:"minerIp"`
	Name           string  `json:"name"`
	Status         string  `json:"status"`
	WorkMode       string  `json:"workMode"`
	Hashrate       float64 `json:"hashrate"`
	Power          float64 `json:"power"`
	Efficiency     float64 `json:"efficiency"`
	TemperatureMax float64 `json:"temperatureMax"`
}

// MinerStatusData holds the list of per-miner status rows
type MinerStatusData struct {
	Miners  []MinerStatusRow `json:"miners"`
	HasData bool             `json:"hasData"`
}

// GetMinerStatuses queries QuestDB for the latest status of each miner.
func (c *Client) GetMinerStatuses() (*MinerStatusData, error) {
	const query = `SELECT timestamp, miner_ip, status, work_mode, hashrate, power, efficiency, temperature_max
  FROM miner_status LATEST ON timestamp PARTITION BY miner_ip;`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query miner statuses: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &MinerStatusData{HasData: false}, nil
	}

	miners := make([]MinerStatusRow, 0, len(result.Dataset))
	for _, row := range result.Dataset {
		if len(row) < 8 {
			continue
		}

		timestamp, _ := row[0].(string)
		minerIP, _ := row[1].(string)
		status, _ := row[2].(string)
		workMode, _ := row[3].(string)

		miners = append(miners, MinerStatusRow{
			Timestamp:      timestamp,
			MinerIP:        minerIP,
			Status:         status,
			WorkMode:       workMode,
			Hashrate:       parseFloat(row[4]),
			Power:          parseFloat(row[5]),
			Efficiency:     parseFloat(row[6]),
			TemperatureMax: parseFloat(row[7]),
		})
	}

	return &MinerStatusData{
		Miners:  miners,
		HasData: len(miners) > 0,
	}, nil
}

// parseFloat extracts a float64 from a JSON-decoded interface value.
func parseFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// ShellyPowerReading represents the latest power reading from a Shelly device
type ShellyPowerReading struct {
	Timestamp string  `json:"timestamp"`
	DeviceID  string  `json:"deviceId"`
	Power     float64 `json:"power"`
}

// ShelliesPowerData holds the latest power readings per device
type ShelliesPowerData struct {
	Devices []ShellyPowerReading `json:"devices"`
	HasData bool                 `json:"hasData"`
}

// GetShelliesPower queries QuestDB for the latest power reading from each Shelly device.
func (c *Client) GetShelliesPower() (*ShelliesPowerData, error) {
	const query = `SELECT timestamp, device_id, power FROM shellies LATEST ON timestamp PARTITION BY device_id;`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query shellies power: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &ShelliesPowerData{HasData: false}, nil
	}

	devices := make([]ShellyPowerReading, 0, len(result.Dataset))
	for _, row := range result.Dataset {
		if len(row) < 3 {
			continue
		}

		timestamp, _ := row[0].(string)
		deviceID, _ := row[1].(string)

		devices = append(devices, ShellyPowerReading{
			Timestamp: timestamp,
			DeviceID:  deviceID,
			Power:     parseFloat(row[2]),
		})
	}

	return &ShelliesPowerData{
		Devices: devices,
		HasData: len(devices) > 0,
	}, nil
}

// LatestEnvironmentReading represents the latest temperature reading for a location
type LatestEnvironmentReading struct {
	Timestamp   string  `json:"timestamp"`
	Location    string  `json:"location"`
	Temperature float64 `json:"temperature"`
}

// LatestEnvironmentData holds the latest readings per location
type LatestEnvironmentData struct {
	Readings []LatestEnvironmentReading `json:"readings"`
	HasData  bool                       `json:"hasData"`
}

// GetLatestEnvironmentTemperatures queries QuestDB for the latest temperature from each location.
func (c *Client) GetLatestEnvironmentTemperatures() (*LatestEnvironmentData, error) {
	const query = `SELECT timestamp, location, temperature FROM bme280_readings LATEST ON timestamp PARTITION BY location;`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest environment temperatures: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &LatestEnvironmentData{HasData: false}, nil
	}

	readings := make([]LatestEnvironmentReading, 0, len(result.Dataset))
	for _, row := range result.Dataset {
		if len(row) < 3 {
			continue
		}

		timestamp, _ := row[0].(string)
		location, _ := row[1].(string)

		readings = append(readings, LatestEnvironmentReading{
			Timestamp:   timestamp,
			Location:    location,
			Temperature: parseFloat(row[2]),
		})
	}

	return &LatestEnvironmentData{
		Readings: readings,
		HasData:  len(readings) > 0,
	}, nil
}

// EnvironmentReading represents a single temperature reading from a sensor
type EnvironmentReading struct {
	Timestamp   string  `json:"timestamp"`
	Location    string  `json:"location"`
	Temperature float64 `json:"temperature"`
}

// EnvironmentChartData represents temperature data grouped by location for charting
type EnvironmentChartData struct {
	Locations map[string][]EnvironmentReading `json:"locations"`
	HasData   bool                            `json:"hasData"`
}

// MinerTemperatureReading represents a single temperature reading from a miner
type MinerTemperatureReading struct {
	Timestamp string  `json:"timestamp"`
	MinerIP   string  `json:"minerIp"`
	Temp0     float64 `json:"temp0"`
	Temp1     float64 `json:"temp1"`
}

// MinerTemperatureChartData represents temperature data grouped by miner IP for charting
type MinerTemperatureChartData struct {
	Miners  map[string][]MinerTemperatureReading `json:"miners"`
	HasData bool                                 `json:"hasData"`
}

// GetMinerTemperatures queries QuestDB for miner temperature readings from the last 24 hours.
func (c *Client) GetMinerTemperatures() (*MinerTemperatureChartData, error) {
	const query = "SELECT timestamp, miner_ip, AVG(temperature_raw_0) as avg_temp0, AVG(temperature_raw_1) as avg_temp1 FROM hashboards WHERE timestamp > dateadd('h', -24, now()) GROUP BY timestamp, miner_ip ORDER BY timestamp;"

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query miner temperatures: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &MinerTemperatureChartData{
			Miners:  make(map[string][]MinerTemperatureReading),
			HasData: false,
		}, nil
	}

	miners := make(map[string][]MinerTemperatureReading)

	for _, row := range result.Dataset {
		if len(row) < 4 {
			continue
		}

		timestamp, ok := row[0].(string)
		if !ok {
			continue
		}

		minerIP, ok := row[1].(string)
		if !ok {
			continue
		}

		reading := MinerTemperatureReading{
			Timestamp: timestamp,
			MinerIP:   minerIP,
			Temp0:     parseFloat(row[2]),
			Temp1:     parseFloat(row[3]),
		}

		miners[minerIP] = append(miners[minerIP], reading)
	}

	return &MinerTemperatureChartData{
		Miners:  miners,
		HasData: len(miners) > 0,
	}, nil
}

// HashboardDetailedRow represents the latest avg voltage and frequency for a single miner
type HashboardDetailedRow struct {
	Timestamp    string  `json:"timestamp"`
	MinerIP      string  `json:"minerIp"`
	AvgVoltage   float64 `json:"avgVoltage"`
	AvgFrequency float64 `json:"avgFrequency"`
}

// HashboardDetailedData holds the latest voltage/frequency readings per miner
type HashboardDetailedData struct {
	Miners  []HashboardDetailedRow `json:"miners"`
	HasData bool                   `json:"hasData"`
}

// GetHashboardsDetailed queries QuestDB for the latest avg voltage and frequency per miner.
func (c *Client) GetHashboardsDetailed() (*HashboardDetailedData, error) {
	const query = `SELECT timestamp, miner_ip, avg(voltage), avg(frequency_avg) FROM hashboards_detailed LATEST ON timestamp PARTITION BY miner_ip;`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query hashboards detailed: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &HashboardDetailedData{HasData: false}, nil
	}

	miners := make([]HashboardDetailedRow, 0, len(result.Dataset))
	for _, row := range result.Dataset {
		if len(row) < 4 {
			continue
		}

		timestamp, _ := row[0].(string)
		minerIP, _ := row[1].(string)

		miners = append(miners, HashboardDetailedRow{
			Timestamp:    timestamp,
			MinerIP:      minerIP,
			AvgVoltage:   parseFloat(row[2]),
			AvgFrequency: parseFloat(row[3]),
		})
	}

	return &HashboardDetailedData{
		Miners:  miners,
		HasData: len(miners) > 0,
	}, nil
}

// GetEnvironmentTemperatures queries QuestDB for environment temperature readings for today,
// using a 10-minute rolling average window per location.
func (c *Client) GetEnvironmentTemperatures() (*EnvironmentChartData, error) {
	const query = `SELECT timestamp, location, avg(temperature) OVER (PARTITION BY location ORDER BY timestamp RANGE BETWEEN '10' MINUTE PRECEDING AND CURRENT ROW) temp FROM bme280_readings WHERE timestamp IN today();`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query environment temperatures: %w", err)
	}

	// Check if we have any data
	if result.Count == 0 || len(result.Dataset) == 0 {
		return &EnvironmentChartData{
			Locations: make(map[string][]EnvironmentReading),
			HasData:   false,
		}, nil
	}

	// Group readings by location
	locations := make(map[string][]EnvironmentReading)

	for _, row := range result.Dataset {
		if len(row) < 3 {
			continue
		}

		// Parse timestamp
		timestamp, ok := row[0].(string)
		if !ok {
			continue
		}

		// Parse location
		location, ok := row[1].(string)
		if !ok {
			continue
		}

		// Parse temperature
		var temperature float64
		switch v := row[2].(type) {
		case float64:
			temperature = v
		case int:
			temperature = float64(v)
		case int64:
			temperature = float64(v)
		default:
			continue
		}

		reading := EnvironmentReading{
			Timestamp:   timestamp,
			Location:    location,
			Temperature: temperature,
		}

		locations[location] = append(locations[location], reading)
	}

	return &EnvironmentChartData{
		Locations: locations,
		HasData:   len(locations) > 0,
	}, nil
}

// HumidityReading represents a single humidity reading from a sensor
type HumidityReading struct {
	Timestamp string  `json:"timestamp"`
	Location  string  `json:"location"`
	Humidity  float64 `json:"humidity"`
}

// HumidityChartData represents humidity data grouped by location for charting
type HumidityChartData struct {
	Locations map[string][]HumidityReading `json:"locations"`
	HasData   bool                         `json:"hasData"`
}

// GetEnvironmentHumidity queries QuestDB for humidity readings for today,
// using a 10-minute rolling average window per location.
func (c *Client) GetEnvironmentHumidity() (*HumidityChartData, error) {
	const query = `SELECT timestamp, location, avg(humidity) OVER (PARTITION BY location ORDER BY timestamp RANGE BETWEEN '10' MINUTE PRECEDING AND CURRENT ROW) humidity FROM bme280_readings WHERE timestamp IN today();`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query environment humidity: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &HumidityChartData{
			Locations: make(map[string][]HumidityReading),
			HasData:   false,
		}, nil
	}

	locations := make(map[string][]HumidityReading)
	for _, row := range result.Dataset {
		if len(row) < 3 {
			continue
		}
		timestamp, ok := row[0].(string)
		if !ok {
			continue
		}
		location, ok := row[1].(string)
		if !ok {
			continue
		}

		locations[location] = append(locations[location], HumidityReading{
			Timestamp: timestamp,
			Location:  location,
			Humidity:  parseFloat(row[2]),
		})
	}

	return &HumidityChartData{
		Locations: locations,
		HasData:   len(locations) > 0,
	}, nil
}

// PressureReading represents a single pressure reading from a sensor
type PressureReading struct {
	Timestamp string  `json:"timestamp"`
	Location  string  `json:"location"`
	Pressure  float64 `json:"pressure"`
}

// PressureChartData represents pressure data grouped by location for charting
type PressureChartData struct {
	Locations map[string][]PressureReading `json:"locations"`
	HasData   bool                         `json:"hasData"`
}

// GetEnvironmentPressure queries QuestDB for pressure readings for today,
// using a 10-minute rolling average window per location.
func (c *Client) GetEnvironmentPressure() (*PressureChartData, error) {
	const query = `SELECT timestamp, location, avg(pressure) OVER (PARTITION BY location ORDER BY timestamp RANGE BETWEEN '10' MINUTE PRECEDING AND CURRENT ROW) pressure FROM bme280_readings WHERE timestamp IN today();`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query environment pressure: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &PressureChartData{
			Locations: make(map[string][]PressureReading),
			HasData:   false,
		}, nil
	}

	locations := make(map[string][]PressureReading)
	for _, row := range result.Dataset {
		if len(row) < 3 {
			continue
		}
		timestamp, ok := row[0].(string)
		if !ok {
			continue
		}
		location, ok := row[1].(string)
		if !ok {
			continue
		}

		locations[location] = append(locations[location], PressureReading{
			Timestamp: timestamp,
			Location:  location,
			Pressure:  parseFloat(row[2]),
		})
	}

	return &PressureChartData{
		Locations: locations,
		HasData:   len(locations) > 0,
	}, nil
}

// HourlyTempRow represents the average temperature for one hour of the day
type HourlyTempRow struct {
	Hour    int     `json:"hour"`
	AvgTemp float64 `json:"avgTemp"`
}

// HourlyTempData holds the hourly average temperature data
type HourlyTempData struct {
	Hours   []HourlyTempRow `json:"hours"`
	HasData bool            `json:"hasData"`
}

// GetHourlyAvgTemperature queries QuestDB for the average miningroom temperature
// by hour of the day over the past 7 days.
func (c *Client) GetHourlyAvgTemperature() (*HourlyTempData, error) {
	const query = `SELECT hour(timestamp) as hour_of_day, AVG(temperature) as avg_temp FROM bme280_readings WHERE timestamp > dateadd('d', -7, now()) AND location='miningroom' GROUP BY hour_of_day ORDER BY hour_of_day;`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query hourly avg temperature: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &HourlyTempData{HasData: false}, nil
	}

	hours := make([]HourlyTempRow, 0, len(result.Dataset))
	for _, row := range result.Dataset {
		if len(row) < 2 {
			continue
		}
		hours = append(hours, HourlyTempRow{
			Hour:    int(parseFloat(row[0])),
			AvgTemp: parseFloat(row[1]),
		})
	}

	return &HourlyTempData{
		Hours:   hours,
		HasData: len(hours) > 0,
	}, nil
}

// ThermalDataPoint represents a single thermal insulation calculation point
type ThermalDataPoint struct {
	Timestamp          string  `json:"timestamp"`
	Power              float64 `json:"power"`
	InsideTemp         float64 `json:"insideTemp"`
	OutsideTemp        float64 `json:"outsideTemp"`
	DeltaT             float64 `json:"deltaT"`
	ThermalConductance float64 `json:"thermalConductance"` // W/K - lower is better insulation
}

// ThermalInsulationData holds the thermal insulation time series
type ThermalInsulationData struct {
	DataPoints []ThermalDataPoint `json:"dataPoints"`
	HasData    bool               `json:"hasData"`
}

// GetThermalInsulationData queries QuestDB for power and temperature data to calculate
// thermal insulation coefficient over time. Uses 10-minute sampling.
func (c *Client) GetThermalInsulationData() (*ThermalInsulationData, error) {
	// Query power data sampled by 10 minutes
	const powerQuery = `SELECT timestamp, sum(power) as total_power FROM shellies WHERE timestamp > dateadd('d', -7, now()) SAMPLE BY 10m ALIGN TO CALENDAR;`

	// Query inside (miningroom) temperature
	const insideQuery = `SELECT timestamp, avg(temperature) as temp FROM bme280_readings WHERE timestamp > dateadd('d', -7, now()) AND location = 'miningroom' SAMPLE BY 10m ALIGN TO CALENDAR;`

	// Query outside temperature
	const outsideQuery = `SELECT timestamp, avg(temperature) as temp FROM bme280_readings WHERE timestamp > dateadd('d', -7, now()) AND location = 'outside' SAMPLE BY 10m ALIGN TO CALENDAR;`

	// Execute all three queries
	powerResult, err := c.Query(powerQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query power data: %w", err)
	}

	insideResult, err := c.Query(insideQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query inside temperature: %w", err)
	}

	outsideResult, err := c.Query(outsideQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query outside temperature: %w", err)
	}

	// Build maps by timestamp
	powerMap := make(map[string]float64)
	for _, row := range powerResult.Dataset {
		if len(row) >= 2 {
			if ts, ok := row[0].(string); ok {
				powerMap[ts] = parseFloat(row[1])
			}
		}
	}

	insideMap := make(map[string]float64)
	for _, row := range insideResult.Dataset {
		if len(row) >= 2 {
			if ts, ok := row[0].(string); ok {
				insideMap[ts] = parseFloat(row[1])
			}
		}
	}

	outsideMap := make(map[string]float64)
	for _, row := range outsideResult.Dataset {
		if len(row) >= 2 {
			if ts, ok := row[0].(string); ok {
				outsideMap[ts] = parseFloat(row[1])
			}
		}
	}

	// Join data points where we have all three values
	var dataPoints []ThermalDataPoint
	for ts, power := range powerMap {
		insideTemp, hasInside := insideMap[ts]
		outsideTemp, hasOutside := outsideMap[ts]

		if hasInside && hasOutside && power > 100 { // Minimum power threshold
			deltaT := insideTemp - outsideTemp
			if deltaT > 1 { // Need meaningful temperature difference
				conductance := power / deltaT
				dataPoints = append(dataPoints, ThermalDataPoint{
					Timestamp:          ts,
					Power:              power,
					InsideTemp:         insideTemp,
					OutsideTemp:        outsideTemp,
					DeltaT:             deltaT,
					ThermalConductance: conductance,
				})
			}
		}
	}

	// Sort by timestamp
	for i := 0; i < len(dataPoints)-1; i++ {
		for j := i + 1; j < len(dataPoints); j++ {
			if dataPoints[i].Timestamp > dataPoints[j].Timestamp {
				dataPoints[i], dataPoints[j] = dataPoints[j], dataPoints[i]
			}
		}
	}

	return &ThermalInsulationData{
		DataPoints: dataPoints,
		HasData:    len(dataPoints) > 0,
	}, nil
}

// DailyEnergyRow represents energy usage for a single day
type DailyEnergyRow struct {
	Date      string  `json:"date"`      // e.g. "2026-02-04"
	EnergyKWh float64 `json:"energyKwh"` // kWh consumed that day
	AvgPowerW float64 `json:"avgPowerW"` // average total power (W)
}

// DailyEnergyData holds the daily energy usage time series
type DailyEnergyData struct {
	Days    []DailyEnergyRow `json:"days"`
	HasData bool             `json:"hasData"`
}

// GetDailyEnergyUsage queries QuestDB for power data over the past 7 days,
// groups by calendar day, and computes average power and energy (kWh) per day.
func (c *Client) GetDailyEnergyUsage() (*DailyEnergyData, error) {
	const query = `SELECT timestamp, sum(power) as total_power FROM shellies WHERE timestamp > dateadd('d', -7, now()) SAMPLE BY 10m ALIGN TO CALENDAR;`

	result, err := c.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query daily energy usage: %w", err)
	}

	if result.Count == 0 || len(result.Dataset) == 0 {
		return &DailyEnergyData{HasData: false}, nil
	}

	// Group power readings by date (first 10 chars of timestamp = "YYYY-MM-DD")
	type dayAccum struct {
		totalPower float64
		count      int
	}
	dayMap := make(map[string]*dayAccum)

	for _, row := range result.Dataset {
		if len(row) < 2 {
			continue
		}
		ts, ok := row[0].(string)
		if !ok || len(ts) < 10 {
			continue
		}
		date := ts[:10]
		power := parseFloat(row[1])

		if acc, exists := dayMap[date]; exists {
			acc.totalPower += power
			acc.count++
		} else {
			dayMap[date] = &dayAccum{totalPower: power, count: 1}
		}
	}

	days := make([]DailyEnergyRow, 0, len(dayMap))
	for date, acc := range dayMap {
		avgPower := acc.totalPower / float64(acc.count)
		energyKWh := avgPower * 24 / 1000
		days = append(days, DailyEnergyRow{
			Date:      date,
			EnergyKWh: energyKWh,
			AvgPowerW: avgPower,
		})
	}

	sort.Slice(days, func(i, j int) bool {
		return days[i].Date < days[j].Date
	})

	return &DailyEnergyData{
		Days:    days,
		HasData: len(days) > 0,
	}, nil
}
