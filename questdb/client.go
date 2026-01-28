package questdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	const query = "SELECT timestamp, temperature FROM bme280_readings WHERE location='washroom' ORDER BY timestamp DESC LIMIT 1;"

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

// GetEnvironmentTemperatures queries QuestDB for all environment temperature readings from the last 24 hours.
func (c *Client) GetEnvironmentTemperatures() (*EnvironmentChartData, error) {
	const query = "SELECT timestamp, location, temperature FROM bme280_readings WHERE timestamp > dateadd('h', -24, now()) ORDER BY timestamp;"

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
