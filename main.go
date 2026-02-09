package main

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"miningRoom/db"
	"miningRoom/questdb"

	"github.com/gin-gonic/gin"
)

var (
	machines      []db.Machine
	database      *db.DB
	questdbClient *questdb.Client
	minerUser     string
	minerPass     string
)

var innerNetwork = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("10.0.0.0/24")
	return n
}()

// isInnerNetwork returns true if the client IP is on the inner network (10.0.0.0/24) or localhost.
func isInnerNetwork(clientIP string) bool {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return innerNetwork.Contains(ip)
}

// networkContextMiddleware sets ShowManage in the gin context based on client IP.
func networkContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("ShowManage", isInnerNetwork(c.ClientIP()))
		c.Next()
	}
}

// requireInnerNetwork returns 404 for clients not on the inner network.
func requireInnerNetwork() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isInnerNetwork(c.ClientIP()) {
			render404(c)
			return
		}
		c.Next()
	}
}

// render404 responds with a styled 404 page for browsers or a JSON body for API calls.
func render404(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.HTML(http.StatusNotFound, "404.html", gin.H{
		"Title":      "Mining Dashboard",
		"ShowManage": c.GetBool("ShowManage"),
	})
	c.Abort()
}

func main() {
	dbPath := flag.String("db-path", "miningroom.db", "SQLite database path")
	questdbHost := flag.String("questdb-host", "localhost", "QuestDB host for metrics")
	questdbPort := flag.Int("questdb-port", 9001, "QuestDB port")
	flag.StringVar(&minerUser, "miner-user", "root", "Miner HTTP digest auth username")
	flag.StringVar(&minerPass, "miner-pass", "root", "Miner HTTP digest auth password")
	flag.Parse()

	log.Printf("Using QuestDB at %s:%d", *questdbHost, *questdbPort)
	questdbClient = questdb.NewClient(*questdbHost, *questdbPort)

	var err error
	database, err = db.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	if err := database.EnsureSchema(); err != nil {
		log.Fatalf("Failed to ensure database schema: %v", err)
	}

	machines, err = database.FetchMachines()
	if err != nil {
		log.Fatalf("Failed to fetch machines: %v", err)
	}
	log.Printf("Loaded %d mining machines from database", len(machines))

	r := gin.Default()

	// Check client network on every request
	r.Use(networkContextMiddleware())

	// Load HTML templates
	r.LoadHTMLGlob("templates/*")

	// Serve static files
	r.Static("/static", "./static")

	// Dashboard route
	r.GET("/", dashboardHandler)
	r.GET("/miners", minersHandler)
	r.GET("/power-mining", powerMiningHandler)
	r.GET("/environment", environmentHandler)
	r.GET("/manage", requireInnerNetwork(), manageHandler)
	r.GET("/settings", requireInnerNetwork(), settingsHandler)

	// API routes for dashboard data
	api := r.Group("/api")
	{
		api.GET("/status", getStatusHandler)
		api.GET("/gauges", getGaugesHandler)
		api.GET("/charts", getChartsHandler)
		api.GET("/charts/environment", getEnvironmentChartHandler)
		api.GET("/charts/miner-temperatures", getMinerTemperatureChartHandler)
		api.GET("/charts/humidity", getHumidityChartHandler)
		api.GET("/charts/pressure", getPressureChartHandler)
		api.GET("/charts/hourly-temp", getHourlyTempChartHandler)
		api.GET("/charts/thermal-insulation", getThermalInsulationChartHandler)
		api.GET("/charts/daily-energy", getDailyEnergyChartHandler)
		api.GET("/miners/status", getMinerStatusHandler)
		api.GET("/environment/latest", getEnvironmentLatestHandler)

		// Manage APIs - inner network only
		manage := api.Group("/", requireInnerNetwork())
		{
			manage.GET("/manage/miners", getManageMinersHandler)

			// Individual miner control
			manage.POST("/miner/power", setMinerPowerHandler)
			manage.POST("/miner/start", startMinerHandler)
			manage.POST("/miner/shutdown", shutdownMinerHandler)

			// Bulk miner control
			manage.POST("/miners/power", setAllMinersPowerHandler)
			manage.POST("/miners/freq", setAllMinersFreqVoltHandler)
			manage.POST("/miners/sleep", setAllMinersSleepHandler)
			manage.POST("/miners/start", startAllMinersHandler)
			manage.POST("/miners/shutdown", shutdownAllMinersHandler)

			// Machine management
			manage.POST("/machines", addMachineHandler)
			manage.DELETE("/machines/:ip", deleteMachineHandler)
		}
	}

	// Catch-all 404 handler
	r.NoRoute(func(c *gin.Context) {
		render404(c)
	})

	r.Run(":8080")
}

// isTimestampRecent checks if the given ISO 8601 timestamp is within the specified duration from now
func isTimestampRecent(timestamp string, maxAge time.Duration) bool {
	// Parse the timestamp (QuestDB returns ISO 8601 format with microseconds)
	t, err := time.Parse("2006-01-02T15:04:05.000000Z", timestamp)
	if err != nil {
		log.Printf("Failed to parse timestamp %q: %v", timestamp, err)
		return false
	}
	return time.Since(t) <= maxAge
}

// fetchNetworkHashrate returns the current Bitcoin network hashrate in H/s.
func fetchNetworkHashrate() (float64, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://mempool.space/api/v1/mining/hashrate/3d")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var data struct {
		CurrentHashrate float64 `json:"currentHashrate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	return data.CurrentHashrate, nil
}

// fetchBTCPriceEUR returns the current BTC price in EUR.
func fetchBTCPriceEUR() (float64, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://mempool.space/api/v1/prices")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var data struct {
		EUR float64 `json:"EUR"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	return data.EUR, nil
}

// calculateDailyRevenueEUR estimates daily mining revenue in EUR.
// myHashrateTH is the miner's hashrate in TH/s.
func calculateDailyRevenueEUR(myHashrateTH float64) float64 {
	if myHashrateTH <= 0 {
		return 0
	}

	const blockRewardBTC = 3.15

	var networkHashrate, btcPriceEUR float64
	var err1, err2 error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		networkHashrate, err1 = fetchNetworkHashrate()
	}()
	go func() {
		defer wg.Done()
		btcPriceEUR, err2 = fetchBTCPriceEUR()
	}()
	wg.Wait()

	if err1 != nil {
		log.Printf("Failed to fetch network hashrate: %v", err1)
		return 0
	}
	if err2 != nil {
		log.Printf("Failed to fetch BTC price: %v", err2)
		return 0
	}

	if networkHashrate <= 0 {
		return 0
	}

	myHashrateHS := myHashrateTH * 1e12
	myShare := myHashrateHS / networkHashrate
	dailyBTC := myShare * 144 * blockRewardBTC
	return math.Round(dailyBTC*btcPriceEUR*100) / 100
}

func dashboardHandler(c *gin.Context) {
	// Get hashrate and status from QuestDB
	online := false
	statusLabel := "No Data"
	hashrate := 0.0
	power := 0.0

	result, err := questdbClient.GetTotalHashrate()
	if err != nil {
		log.Printf("Failed to get hashrate from QuestDB: %v", err)
	} else if result.HasData {
		online = isTimestampRecent(result.Timestamp, 5*time.Minute)
		if online {
			statusLabel = "Online"
		} else {
			statusLabel = "Stale Data"
		}
		hashrate = result.TotalHashrate / 1000 // Convert GH/s to TH/s
	}

	// Get total power from QuestDB
	powerResult, err := questdbClient.GetTotalPower()
	if err != nil {
		log.Printf("Failed to get power from QuestDB: %v", err)
	} else if powerResult.HasData {
		power = powerResult.TotalPower
	}

	// Calculate efficiency (J/TH) - Joules per Terahash
	efficiency := 0.0
	if hashrate > 0 {
		efficiency = power / hashrate // W / (TH/s) = J/TH
	}

	// Calculate daily revenue in EUR
	revenue := calculateDailyRevenueEUR(hashrate)

	// Calculate daily electricity cost: power(W) / 1000 * 24h * €0.23/kWh
	elecCost := math.Round(power/1000*24*0.23*100) / 100

	// Round values for display
	hashrate = math.Round(hashrate)
	efficiency = math.Round(efficiency*10) / 10 // 1 decimal
	power = math.Round(power)

	data := gin.H{
		"Title":      "Mining Dashboard",
		"Machines":   machines,
		"ShowManage": c.GetBool("ShowManage"),
		"Status": gin.H{
			"Online": online,
			"Label":  statusLabel,
		},
		"Gauges": []gin.H{
			{"Label": "Power", "Value": power, "Unit": "W"},
			{"Label": "Hashrate", "Value": hashrate, "Unit": "TH/s"},
			{"Label": "Efficiency", "Value": efficiency, "Unit": "J/TH"},
			{"Label": "Elec. Cost", "Value": elecCost, "Unit": "€/day"},
			{"Label": "Revenue", "Value": revenue, "Unit": "€/day"},
		},
	}

	c.HTML(http.StatusOK, "dashboard.html", data)
}

func getStatusHandler(c *gin.Context) {
	result, err := questdbClient.GetTotalHashrate()
	if err != nil {
		log.Printf("Failed to get hashrate from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"online":      false,
			"label":       "No Data",
			"hashrate":    0,
			"temperature": 0,
			"roomTemp":    0,
			"power":       0,
			"efficiency":  0,
		})
		return
	}

	if !result.HasData {
		c.JSON(http.StatusOK, gin.H{
			"online":      false,
			"label":       "No Data",
			"hashrate":    0,
			"temperature": 0,
			"roomTemp":    0,
			"power":       0,
			"efficiency":  0,
		})
		return
	}

	// Check if the timestamp is recent (within last 5 minutes)
	online := isTimestampRecent(result.Timestamp, 5*time.Minute)
	label := "Online"
	if !online {
		label = "Stale Data"
	}

	// Get max temperature
	temperature := 0.0
	tempResult, err := questdbClient.GetMaxTemperature()
	if err != nil {
		log.Printf("Failed to get temperature from QuestDB: %v", err)
	} else if tempResult.HasData {
		temperature = tempResult.MaxTemperature
	}

	// Get room temperature
	roomTemp := 0.0
	roomTempResult, err := questdbClient.GetRoomTemperature()
	if err != nil {
		log.Printf("Failed to get room temperature from QuestDB: %v", err)
	} else if roomTempResult.HasData {
		roomTemp = roomTempResult.Temperature
	}

	// Get total power
	power := 0.0
	powerResult, err := questdbClient.GetTotalPower()
	if err != nil {
		log.Printf("Failed to get power from QuestDB: %v", err)
	} else if powerResult.HasData {
		power = powerResult.TotalPower
	}

	// Calculate efficiency (J/TH) - Joules per Terahash
	hashrateTH := result.TotalHashrate / 1000 // Convert GH/s to TH/s
	efficiency := 0.0
	if hashrateTH > 0 {
		efficiency = power / hashrateTH // W / (TH/s) = J/TH
	}

	c.JSON(http.StatusOK, gin.H{
		"online":      online,
		"label":       label,
		"hashrate":    result.TotalHashrate,
		"temperature": temperature,
		"roomTemp":    roomTemp,
		"power":       power,
		"efficiency":  efficiency,
		"timestamp":   result.Timestamp,
	})
}

func getGaugesHandler(c *gin.Context) {
	hashrate := 0.0
	power := 0.0

	result, err := questdbClient.GetTotalHashrate()
	if err != nil {
		log.Printf("Failed to get hashrate from QuestDB: %v", err)
	} else if result.HasData {
		hashrate = result.TotalHashrate / 1000 // Convert GH/s to TH/s
	}

	powerResult, err := questdbClient.GetTotalPower()
	if err != nil {
		log.Printf("Failed to get power from QuestDB: %v", err)
	} else if powerResult.HasData {
		power = powerResult.TotalPower
	}

	// Calculate efficiency (J/TH) - Joules per Terahash
	efficiency := 0.0
	if hashrate > 0 {
		efficiency = power / hashrate // W / (TH/s) = J/TH
	}

	// Calculate daily revenue in EUR
	revenue := calculateDailyRevenueEUR(hashrate)

	// Calculate daily electricity cost: power(W) / 1000 * 24h * €0.23/kWh
	elecCost := math.Round(power/1000*24*0.23*100) / 100

	// Round values for display
	hashrate = math.Round(hashrate)
	efficiency = math.Round(efficiency*10) / 10 // 1 decimal
	power = math.Round(power)

	c.JSON(http.StatusOK, gin.H{
		"gauges": []gin.H{
			{"label": "Power", "value": power, "unit": "W"},
			{"label": "Hashrate", "value": hashrate, "unit": "TH/s"},
			{"label": "Efficiency", "value": efficiency, "unit": "J/TH"},
			{"label": "Elec. Cost", "value": elecCost, "unit": "€/day"},
			{"label": "Revenue", "value": revenue, "unit": "€/day"},
		},
	})
}

func getChartsHandler(c *gin.Context) {
	// Placeholder for charts API
	c.JSON(http.StatusOK, gin.H{
		"message": "Chart data will be provided via queries",
	})
}

func getEnvironmentChartHandler(c *gin.Context) {
	result, err := questdbClient.GetEnvironmentTemperatures()
	if err != nil {
		log.Printf("Failed to get environment temperatures from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"locations": map[string][]interface{}{},
			"hasData":   false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getMinerTemperatureChartHandler(c *gin.Context) {
	result, err := questdbClient.GetMinerTemperatures()
	if err != nil {
		log.Printf("Failed to get miner temperatures from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"miners":  map[string][]interface{}{},
			"hasData": false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getHumidityChartHandler(c *gin.Context) {
	result, err := questdbClient.GetEnvironmentHumidity()
	if err != nil {
		log.Printf("Failed to get humidity from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"locations": map[string][]interface{}{},
			"hasData":   false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getPressureChartHandler(c *gin.Context) {
	result, err := questdbClient.GetEnvironmentPressure()
	if err != nil {
		log.Printf("Failed to get pressure from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"locations": map[string][]interface{}{},
			"hasData":   false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getHourlyTempChartHandler(c *gin.Context) {
	result, err := questdbClient.GetHourlyAvgTemperature()
	if err != nil {
		log.Printf("Failed to get hourly avg temperature from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"hours":   []interface{}{},
			"hasData": false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getThermalInsulationChartHandler(c *gin.Context) {
	result, err := questdbClient.GetThermalInsulationData()
	if err != nil {
		log.Printf("Failed to get thermal insulation data from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"dataPoints": []interface{}{},
			"hasData":    false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getDailyEnergyChartHandler(c *gin.Context) {
	result, err := questdbClient.GetDailyEnergyUsage()
	if err != nil {
		log.Printf("Failed to get daily energy usage from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"days":    []interface{}{},
			"hasData": false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getEnvironmentLatestHandler(c *gin.Context) {
	result, err := questdbClient.GetLatestEnvironmentTemperatures()
	if err != nil {
		log.Printf("Failed to get latest environment temperatures from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"readings": []interface{}{},
			"hasData":  false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func getMinerStatusHandler(c *gin.Context) {
	result, err := questdbClient.GetMinerStatuses()
	if err != nil {
		log.Printf("Failed to get miner statuses from QuestDB: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"miners":  []interface{}{},
			"hasData": false,
		})
		return
	}

	// Build IP to name map from machines
	ipToName := make(map[string]string)
	for _, m := range machines {
		ipToName[m.IP] = m.Name
	}

	// Add names to miner status rows
	for i := range result.Miners {
		if name, ok := ipToName[result.Miners[i].MinerIP]; ok {
			result.Miners[i].Name = name
		} else {
			result.Miners[i].Name = result.Miners[i].MinerIP // fallback to IP
		}
	}

	// Sort by name
	sort.Slice(result.Miners, func(i, j int) bool {
		return result.Miners[i].Name < result.Miners[j].Name
	})

	c.JSON(http.StatusOK, result)
}

// MinerManageInfo represents the parsed config and status for a miner on the manage page.
type MinerManageInfo struct {
	Name                string   `json:"name"`
	IP                  string   `json:"ip"`
	ShellyIP            string   `json:"shellyIp"`
	Online              bool     `json:"online"`
	WorkMode            string   `json:"workMode"`
	ModeSelect          string   `json:"modeSelect"`
	TargetValue         float64  `json:"targetValue"`
	TargetFreq          float64  `json:"targetFreq"`
	TargetVolt          float64  `json:"targetVolt"`
	ModeSelectAvailable []string `json:"modeSelectAvailable"`
}

// camelToKebab converts PascalCase to kebab-case, e.g. "PowerTarget" -> "power-target".
func camelToKebab(s string) string {
	var result []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '-')
			}
			result = append(result, byte(c-'A'+'a'))
		} else {
			result = append(result, byte(c))
		}
	}
	return string(result)
}

// fetchMinerConfig calls a miner's kaonsu API and parses the mode section.
func fetchMinerConfig(ip string) (*MinerManageInfo, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/kaonsu/v1/miner_config", ip))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, err
	}

	info := &MinerManageInfo{Online: true}

	modeObj, _ := config["mode"].(map[string]interface{})
	if modeObj == nil {
		return info, nil
	}

	info.WorkMode, _ = modeObj["work-mode-selector"].(string)

	if info.WorkMode == "Auto" {
		concorde, _ := modeObj["concorde"].(map[string]interface{})
		if concorde == nil {
			return info, nil
		}

		info.ModeSelect, _ = concorde["mode-select"].(string)

		if avail, ok := concorde["mode-select-available"].([]interface{}); ok {
			for _, v := range avail {
				if s, ok := v.(string); ok {
					info.ModeSelectAvailable = append(info.ModeSelectAvailable, s)
				}
			}
		}

		// Derive the target key from mode-select, e.g. "PowerTarget" -> "power-target"
		if info.ModeSelect != "" {
			targetKey := camelToKebab(info.ModeSelect)
			if val, ok := concorde[targetKey].(float64); ok {
				info.TargetValue = val
			}
		}
	} else if info.WorkMode == "Fixed" {
		fixed, _ := modeObj["fixed"].(map[string]interface{})
		if fixed != nil {
			if freq, ok := fixed["freq"].(float64); ok {
				info.TargetFreq = freq
			}
			if volt, ok := fixed["volt"].(float64); ok {
				info.TargetVolt = volt
			}
		}
	}

	return info, nil
}

func getManageMinersHandler(c *gin.Context) {
	results := make([]MinerManageInfo, len(machines))
	var wg sync.WaitGroup

	for i, m := range machines {
		wg.Add(1)
		go func(idx int, machine db.Machine) {
			defer wg.Done()
			info, err := fetchMinerConfig(machine.IP)
			if err != nil {
				log.Printf("Failed to fetch config for %s (%s): %v", machine.Name, machine.IP, err)
				results[idx] = MinerManageInfo{
					Name:     machine.Name,
					IP:       machine.IP,
					ShellyIP: machine.ShellyIP,
					Online:   false,
				}
				return
			}
			info.Name = machine.Name
			info.IP = machine.IP
			info.ShellyIP = machine.ShellyIP
			results[idx] = *info
		}(i, m)
	}

	wg.Wait()

	shelliesData, err := questdbClient.GetShelliesPower()
	if err != nil {
		log.Printf("Failed to get shellies power: %v", err)
		shelliesData = &questdb.ShelliesPowerData{HasData: false}
	}

	minerStatuses, err := questdbClient.GetMinerStatuses()
	if err != nil {
		log.Printf("Failed to get miner statuses: %v", err)
	}

	hashboardsDetailed, err := questdbClient.GetHashboardsDetailed()
	if err != nil {
		log.Printf("Failed to get hashboards detailed: %v", err)
		hashboardsDetailed = &questdb.HashboardDetailedData{HasData: false}
	}

	c.JSON(http.StatusOK, gin.H{
		"miners":             results,
		"shellies":           shelliesData,
		"minerStatuses":      minerStatuses,
		"hashboardsDetailed": hashboardsDetailed,
	})
}

func environmentHandler(c *gin.Context) {
	data := gin.H{
		"Title":      "Mining Dashboard",
		"ShowManage": c.GetBool("ShowManage"),
	}
	c.HTML(http.StatusOK, "environment.html", data)
}

func minersHandler(c *gin.Context) {
	hashrate := 0.0
	power := 0.0
	avgTemp := 0.0
	activeMiners := 0

	// Count active miners: those with a miner_status record in the last 2 minutes
	statusResult, err := questdbClient.GetMinerStatuses()
	if err != nil {
		log.Printf("Failed to get miner statuses: %v", err)
	} else if statusResult.HasData {
		for _, m := range statusResult.Miners {
			if isTimestampRecent(m.Timestamp, 2*time.Minute) {
				activeMiners++
			}
		}
	}

	result, err := questdbClient.GetTotalHashrate()
	if err != nil {
		log.Printf("Failed to get hashrate from QuestDB: %v", err)
	} else if result.HasData {
		hashrate = result.TotalHashrate / 1000 // GH/s to TH/s
	}

	powerResult, err := questdbClient.GetTotalPower()
	if err != nil {
		log.Printf("Failed to get power from QuestDB: %v", err)
	} else if powerResult.HasData {
		power = powerResult.TotalPower
	}

	avgTempResult, err := questdbClient.GetAvgMaxTemperature()
	if err != nil {
		log.Printf("Failed to get avg max temperature from QuestDB: %v", err)
	} else if avgTempResult.HasData {
		avgTemp = avgTempResult.AvgTemperature
	}

	efficiency := 0.0
	if hashrate > 0 {
		efficiency = power / hashrate // J/TH
	}

	hashrate = math.Round(hashrate)
	power = math.Round(power)
	efficiency = math.Round(efficiency*10) / 10
	avgTemp = math.Round(avgTemp*10) / 10

	data := gin.H{
		"Title":      "Mining Dashboard",
		"Machines":   machines,
		"ShowManage": c.GetBool("ShowManage"),
		"Metrics": []gin.H{
			{"Label": "Active Miners", "Value": activeMiners, "Unit": "online", "Color": "success"},
			{"Label": "Total Hashrate", "Value": hashrate, "Unit": "TH/s", "Color": "primary"},
			{"Label": "Total Power", "Value": power, "Unit": "W", "Color": "warning"},
			{"Label": "Efficiency", "Value": efficiency, "Unit": "J/TH", "Color": "info"},
			{"Label": "Avg Temperature", "Value": avgTemp, "Unit": "°C", "Color": "danger"},
			{"Label": "Uptime", "Value": "99.8", "Unit": "%", "Color": "secondary"},
		},
	}
	c.HTML(http.StatusOK, "miners.html", data)
}

func manageHandler(c *gin.Context) {
	data := gin.H{
		"Title":      "Mining Dashboard",
		"Machines":   machines,
		"ShowManage": true,
	}
	c.HTML(http.StatusOK, "manage.html", data)
}

func settingsHandler(c *gin.Context) {
	data := gin.H{
		"Title":      "Mining Dashboard",
		"Machines":   machines,
		"ShowManage": true,
	}
	c.HTML(http.StatusOK, "settings.html", data)
}

func powerMiningHandler(c *gin.Context) {
	data := gin.H{
		"Title":      "Mining Dashboard",
		"Machines":   machines,
		"ShowManage": c.GetBool("ShowManage"),
		"Status": gin.H{
			"Online": true,
			"Label":  "Mining Status",
		},
		"Gauges": []gin.H{
			{"Label": "Total Power", "Value": 2510, "Unit": "W"},
			{"Label": "Hashrate", "Value": 375.5, "Unit": "MH/s"},
			{"Label": "Efficiency", "Value": 0.149, "Unit": "MH/W"},
			{"Label": "Cost/Day", "Value": 6.02, "Unit": "$"},
			{"Label": "Revenue/Day", "Value": 12.50, "Unit": "$"},
		},
	}
	c.HTML(http.StatusOK, "power-mining.html", data)
}

// Machine management handlers

type AddMachineRequest struct {
	Name     string `json:"name" binding:"required"`
	IP       string `json:"ip" binding:"required"`
	ShellyIP string `json:"shellyIp"`
}

func addMachineHandler(c *gin.Context) {
	var req AddMachineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.AddMachine(req.Name, req.IP, req.ShellyIP); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add machine"})
		return
	}

	// Refresh machines list
	var err error
	machines, err = database.FetchMachines()
	if err != nil {
		log.Printf("Failed to refresh machines: %v", err)
	}

	log.Printf("Added machine %s (%s)", req.Name, req.IP)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"name":    req.Name,
		"ip":      req.IP,
	})
}

func deleteMachineHandler(c *gin.Context) {
	ip := c.Param("ip")
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "IP address required"})
		return
	}

	if err := database.DeleteMachine(ip); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete machine"})
		return
	}

	// Refresh machines list
	var err error
	machines, err = database.FetchMachines()
	if err != nil {
		log.Printf("Failed to refresh machines: %v", err)
	}

	log.Printf("Deleted machine with IP %s", ip)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"ip":      ip,
	})
}

// HTTP Digest Authentication helpers

func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomCnonce() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// parseDigestChallenge extracts fields from a WWW-Authenticate: Digest header.
func parseDigestChallenge(header string) map[string]string {
	result := make(map[string]string)
	header = strings.TrimPrefix(header, "Digest ")
	// Split on ", " but be careful with quoted values containing commas
	var parts []string
	var current strings.Builder
	inQuote := false
	for _, c := range header {
		if c == '"' {
			inQuote = !inQuote
			current.WriteRune(c)
		} else if c == ',' && !inQuote {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteRune(c)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	for _, part := range parts {
		part = strings.TrimSpace(part)
		idx := strings.Index(part, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		value = strings.Trim(value, `"`)
		result[key] = value
	}
	return result
}

// doDigestPost sends a POST request with HTTP Digest Authentication.
// It first attempts the request unauthenticated, and on a 401 computes the
// digest response from the server's challenge and retries.
func doDigestPost(url, username, password string, body []byte) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: send without auth to get the challenge
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	resp.Body.Close()

	// Step 2: parse challenge
	challenge := parseDigestChallenge(wwwAuth)
	realm := challenge["realm"]
	nonce := challenge["nonce"]
	qop := challenge["qop"]
	// qop may contain multiple values; pick "auth"
	if strings.Contains(qop, "auth") {
		qop = "auth"
	}

	// Step 3: compute digest
	cnonce := randomCnonce()
	nc := "00000001"
	uri := req.URL.RequestURI()

	ha1 := md5Hash(username + ":" + realm + ":" + password)
	ha2 := md5Hash("POST:" + uri)
	var response string
	if qop == "auth" {
		response = md5Hash(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5Hash(ha1 + ":" + nonce + ":" + ha2)
	}

	authHeader := fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", algorithm=MD5, response="%s", qop=%s, nc=%s, cnonce="%s"`,
		username, realm, nonce, uri, response, qop, nc, cnonce,
	)

	// Step 4: retry with Authorization
	req2, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", authHeader)

	return client.Do(req2)
}

// setMinerPowerTarget GETs the current config from a miner, sets the power target,
// and POSTs it back using HTTP Digest Auth.
func setMinerPowerTarget(ip string, power int) error {
	configURL := fmt.Sprintf("http://%s/kaonsu/v1/miner_config", ip)

	// GET current config
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(configURL)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Modify power-target in mode.concorde
	modeObj, _ := config["mode"].(map[string]interface{})
	if modeObj == nil {
		return fmt.Errorf("no mode section in config")
	}

	// Set work mode to Auto for power target to take effect
	modeObj["work-mode-selector"] = "Auto"

	concorde, _ := modeObj["concorde"].(map[string]interface{})
	if concorde == nil {
		return fmt.Errorf("no concorde section in config")
	}

	concorde["mode-select"] = "PowerTarget"
	concorde["power-target"] = power

	// POST modified config with digest auth
	modifiedBody, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	postResp, err := doDigestPost(configURL, minerUser, minerPass, modifiedBody)
	if err != nil {
		return fmt.Errorf("failed to post config: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(postResp.Body)
		return fmt.Errorf("miner returned status %d: %s", postResp.StatusCode, string(respBody))
	}

	return nil
}

// Shelly Pro 1PM relay control (Gen2 RPC API)

// shellyIPForMiner looks up the Shelly IP associated with a miner IP.
func shellyIPForMiner(minerIP string) string {
	for _, m := range machines {
		if m.IP == minerIP {
			return m.ShellyIP
		}
	}
	return ""
}

// getShellyStatus returns the current on/off state of a Shelly switch.
func getShellyStatus(shellyIP string) (bool, error) {
	url := fmt.Sprintf("http://%s/rpc/Switch.GetStatus?id=0", shellyIP)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false, fmt.Errorf("failed to reach shelly at %s: %w", shellyIP, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("shelly %s returned status %d: %s", shellyIP, resp.StatusCode, string(body))
	}

	var status struct {
		Output bool `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false, fmt.Errorf("failed to decode shelly status: %w", err)
	}

	return status.Output, nil
}

// toggleShelly sends a toggle command to a Shelly switch.
func toggleShelly(shellyIP string) error {
	url := fmt.Sprintf("http://%s/rpc/Switch.Toggle?id=0", shellyIP)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to reach shelly at %s: %w", shellyIP, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("shelly %s returned status %d: %s", shellyIP, resp.StatusCode, string(body))
	}
	return nil
}

// controlShelly turns a Shelly Pro 1PM relay on or off via its Gen2 RPC API.
// It first checks the current state and only toggles if needed.
func controlShelly(shellyIP string, on bool) error {
	currentState, err := getShellyStatus(shellyIP)
	if err != nil {
		return err
	}

	// Only toggle if current state differs from desired state
	if currentState == on {
		// Already in desired state, nothing to do
		log.Printf("Shelly %s already %s, skipping toggle", shellyIP, map[bool]string{true: "on", false: "off"}[on])
		return nil
	}

	return toggleShelly(shellyIP)
}

// Individual miner control handlers

type MinerPowerRequest struct {
	IP    string `json:"ip"`
	Power int    `json:"power"`
}

type MinerRequest struct {
	IP string `json:"ip"`
}

type BulkPowerRequest struct {
	IPs   []string `json:"ips"`
	Power int      `json:"power"`
}

type BulkMinerRequest struct {
	IPs []string `json:"ips"`
}

func setMinerPowerHandler(c *gin.Context) {
	var req MinerPowerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := setMinerPowerTarget(req.IP, req.Power); err != nil {
		log.Printf("Failed to set power for %s: %v", req.IP, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Set power to %d W for miner at %s", req.Power, req.IP)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"ip":      req.IP,
		"power":   req.Power,
	})
}

func startMinerHandler(c *gin.Context) {
	var req MinerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	shellyIP := shellyIPForMiner(req.IP)
	if shellyIP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no shelly configured for " + req.IP})
		return
	}

	if err := controlShelly(shellyIP, true); err != nil {
		log.Printf("Failed to start miner %s via shelly %s: %v", req.IP, shellyIP, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Started miner at %s (shelly %s)", req.IP, shellyIP)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"ip":      req.IP,
	})
}

func shutdownMinerHandler(c *gin.Context) {
	var req MinerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	shellyIP := shellyIPForMiner(req.IP)
	if shellyIP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no shelly configured for " + req.IP})
		return
	}

	if err := controlShelly(shellyIP, false); err != nil {
		log.Printf("Failed to shutdown miner %s via shelly %s: %v", req.IP, shellyIP, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Shutdown miner at %s (shelly %s)", req.IP, shellyIP)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"ip":      req.IP,
	})
}

// Bulk miner control handlers

func setAllMinersPowerHandler(c *gin.Context) {
	var req BulkPowerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string

	for _, ip := range req.IPs {
		wg.Add(1)
		go func(minerIP string) {
			defer wg.Done()
			if err := setMinerPowerTarget(minerIP, req.Power); err != nil {
				log.Printf("Failed to set power for %s: %v", minerIP, err)
				mu.Lock()
				failed = append(failed, minerIP)
				mu.Unlock()
			} else {
				log.Printf("Set power to %d W for miner at %s", req.Power, minerIP)
			}
		}(ip)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"success": len(failed) == 0,
		"power":   req.Power,
		"ips":     req.IPs,
		"count":   len(req.IPs),
		"failed":  failed,
	})
}

// setMinerFreqVolt GETs the current config, sets work-mode-selector to "Fixed"
// and writes freq/volt into the fixed section, then POSTs with digest auth.
func setMinerFreqVolt(ip string, freq float64, volt float64) error {
	configURL := fmt.Sprintf("http://%s/kaonsu/v1/miner_config", ip)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(configURL)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	modeObj, _ := config["mode"].(map[string]interface{})
	if modeObj == nil {
		return fmt.Errorf("no mode section in config")
	}

	modeObj["work-mode-selector"] = "Fixed"

	fixed, _ := modeObj["fixed"].(map[string]interface{})
	if fixed == nil {
		fixed = make(map[string]interface{})
		modeObj["fixed"] = fixed
	}

	fixed["freq"] = freq
	fixed["volt"] = volt

	modifiedBody, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	postResp, err := doDigestPost(configURL, minerUser, minerPass, modifiedBody)
	if err != nil {
		return fmt.Errorf("failed to post config: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(postResp.Body)
		return fmt.Errorf("miner returned status %d: %s", postResp.StatusCode, string(respBody))
	}

	return nil
}

type BulkFreqVoltRequest struct {
	IPs  []string `json:"ips"`
	Freq float64  `json:"freq"`
	Volt float64  `json:"volt"`
}

// setMinerSleepMode GETs the current config, sets work-mode-selector to "Sleep",
// then POSTs with digest auth.
func setMinerSleepMode(ip string) error {
	configURL := fmt.Sprintf("http://%s/kaonsu/v1/miner_config", ip)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(configURL)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	modeObj, _ := config["mode"].(map[string]interface{})
	if modeObj == nil {
		return fmt.Errorf("no mode section in config")
	}

	modeObj["work-mode-selector"] = "Sleep"

	modifiedBody, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	postResp, err := doDigestPost(configURL, minerUser, minerPass, modifiedBody)
	if err != nil {
		return fmt.Errorf("failed to post config: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(postResp.Body)
		return fmt.Errorf("miner returned status %d: %s", postResp.StatusCode, string(respBody))
	}

	return nil
}

func setAllMinersFreqVoltHandler(c *gin.Context) {
	var req BulkFreqVoltRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string

	for _, ip := range req.IPs {
		wg.Add(1)
		go func(minerIP string) {
			defer wg.Done()
			if err := setMinerFreqVolt(minerIP, req.Freq, req.Volt); err != nil {
				log.Printf("Failed to set freq/volt for %s: %v", minerIP, err)
				mu.Lock()
				failed = append(failed, minerIP)
				mu.Unlock()
			} else {
				log.Printf("Set freq=%.0f MHz volt=%.1f V for miner at %s", req.Freq, req.Volt, minerIP)
			}
		}(ip)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"success": len(failed) == 0,
		"freq":    req.Freq,
		"volt":    req.Volt,
		"ips":     req.IPs,
		"count":   len(req.IPs),
		"failed":  failed,
	})
}

func setAllMinersSleepHandler(c *gin.Context) {
	var req BulkMinerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string

	for _, ip := range req.IPs {
		wg.Add(1)
		go func(minerIP string) {
			defer wg.Done()
			if err := setMinerSleepMode(minerIP); err != nil {
				log.Printf("Failed to set sleep mode for %s: %v", minerIP, err)
				mu.Lock()
				failed = append(failed, minerIP)
				mu.Unlock()
			} else {
				log.Printf("Set sleep mode for miner at %s", minerIP)
			}
		}(ip)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"success": len(failed) == 0,
		"ips":     req.IPs,
		"count":   len(req.IPs),
		"failed":  failed,
	})
}

func startAllMinersHandler(c *gin.Context) {
	var req BulkMinerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string

	for _, ip := range req.IPs {
		wg.Add(1)
		go func(minerIP string) {
			defer wg.Done()
			shellyIP := shellyIPForMiner(minerIP)
			if shellyIP == "" {
				log.Printf("No shelly configured for %s", minerIP)
				mu.Lock()
				failed = append(failed, minerIP)
				mu.Unlock()
				return
			}
			if err := controlShelly(shellyIP, true); err != nil {
				log.Printf("Failed to start miner %s via shelly %s: %v", minerIP, shellyIP, err)
				mu.Lock()
				failed = append(failed, minerIP)
				mu.Unlock()
			} else {
				log.Printf("Started miner at %s (shelly %s)", minerIP, shellyIP)
			}
		}(ip)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"success": len(failed) == 0,
		"ips":     req.IPs,
		"count":   len(req.IPs),
		"failed":  failed,
	})
}

func shutdownAllMinersHandler(c *gin.Context) {
	var req BulkMinerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string

	for _, ip := range req.IPs {
		wg.Add(1)
		go func(minerIP string) {
			defer wg.Done()
			shellyIP := shellyIPForMiner(minerIP)
			if shellyIP == "" {
				log.Printf("No shelly configured for %s", minerIP)
				mu.Lock()
				failed = append(failed, minerIP)
				mu.Unlock()
				return
			}
			if err := controlShelly(shellyIP, false); err != nil {
				log.Printf("Failed to shutdown miner %s via shelly %s: %v", minerIP, shellyIP, err)
				mu.Lock()
				failed = append(failed, minerIP)
				mu.Unlock()
			} else {
				log.Printf("Shutdown miner at %s (shelly %s)", minerIP, shellyIP)
			}
		}(ip)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"success": len(failed) == 0,
		"ips":     req.IPs,
		"count":   len(req.IPs),
		"failed":  failed,
	})
}
