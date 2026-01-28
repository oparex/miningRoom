package main

import (
	"flag"
	"log"
	"math"
	"net/http"
	"time"

	"miningRoom/db"
	"miningRoom/questdb"

	"github.com/gin-gonic/gin"
)

var (
	machines      []db.Machine
	database      *db.DB
	questdbClient *questdb.Client
)

func main() {
	dbPath := flag.String("db-path", "miningroom.db", "SQLite database path")
	questdbHost := flag.String("questdb-host", "localhost", "QuestDB host for metrics")
	questdbPort := flag.Int("questdb-port", 9001, "QuestDB port")
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

	// Load HTML templates
	r.LoadHTMLGlob("templates/*")

	// Serve static files
	r.Static("/static", "./static")

	// Dashboard route
	r.GET("/", dashboardHandler)
	r.GET("/miners", minersHandler)
	r.GET("/power-mining", powerMiningHandler)
	r.GET("/environment", environmentHandler)
	r.GET("/manage", manageHandler)
	r.GET("/settings", settingsHandler)

	// API routes for dashboard data (placeholder for future queries)
	api := r.Group("/api")
	{
		api.GET("/status", getStatusHandler)
		api.GET("/gauges", getGaugesHandler)
		api.GET("/charts", getChartsHandler)
		api.GET("/charts/environment", getEnvironmentChartHandler)

		// Individual miner control
		api.POST("/miner/power", setMinerPowerHandler)
		api.POST("/miner/start", startMinerHandler)
		api.POST("/miner/shutdown", shutdownMinerHandler)

		// Bulk miner control
		api.POST("/miners/power", setAllMinersPowerHandler)
		api.POST("/miners/start", startAllMinersHandler)
		api.POST("/miners/shutdown", shutdownAllMinersHandler)

		// Machine management
		api.POST("/machines", addMachineHandler)
		api.DELETE("/machines/:ip", deleteMachineHandler)
	}

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

func dashboardHandler(c *gin.Context) {
	// Get hashrate and status from QuestDB
	online := false
	statusLabel := "No Data"
	hashrate := 0.0
	temperature := 0.0
	roomTemp := 0.0
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

	// Get max temperature from QuestDB
	tempResult, err := questdbClient.GetMaxTemperature()
	if err != nil {
		log.Printf("Failed to get temperature from QuestDB: %v", err)
	} else if tempResult.HasData {
		temperature = tempResult.MaxTemperature
	}

	// Get room temperature from QuestDB
	roomTempResult, err := questdbClient.GetRoomTemperature()
	if err != nil {
		log.Printf("Failed to get room temperature from QuestDB: %v", err)
	} else if roomTempResult.HasData {
		roomTemp = roomTempResult.Temperature
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

	// Round values for display
	hashrate = math.Round(hashrate)
	efficiency = math.Round(efficiency*10) / 10   // 1 decimal
	temperature = math.Round(temperature*10) / 10 // 1 decimal
	roomTemp = math.Round(roomTemp*10) / 10       // 1 decimal
	power = math.Round(power)

	data := gin.H{
		"Title":    "Mining Dashboard",
		"Machines": machines,
		"Status": gin.H{
			"Online": online,
			"Label":  statusLabel,
		},
		"Gauges": []gin.H{
			{"Label": "Hashrate", "Value": hashrate, "Unit": "TH/s"},
			{"Label": "Miner Temp", "Value": temperature, "Unit": "°C"},
			{"Label": "Room Temp", "Value": roomTemp, "Unit": "°C"},
			{"Label": "Power", "Value": power, "Unit": "W"},
			{"Label": "Efficiency", "Value": efficiency, "Unit": "J/TH"},
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
	temperature := 0.0
	roomTemp := 0.0
	power := 0.0

	result, err := questdbClient.GetTotalHashrate()
	if err != nil {
		log.Printf("Failed to get hashrate from QuestDB: %v", err)
	} else if result.HasData {
		hashrate = result.TotalHashrate / 1000 // Convert GH/s to TH/s
	}

	tempResult, err := questdbClient.GetMaxTemperature()
	if err != nil {
		log.Printf("Failed to get temperature from QuestDB: %v", err)
	} else if tempResult.HasData {
		temperature = tempResult.MaxTemperature
	}

	roomTempResult, err := questdbClient.GetRoomTemperature()
	if err != nil {
		log.Printf("Failed to get room temperature from QuestDB: %v", err)
	} else if roomTempResult.HasData {
		roomTemp = roomTempResult.Temperature
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

	// Round values for display
	hashrate = math.Round(hashrate)
	efficiency = math.Round(efficiency*10) / 10   // 1 decimal
	temperature = math.Round(temperature*10) / 10 // 1 decimal
	roomTemp = math.Round(roomTemp*10) / 10       // 1 decimal
	power = math.Round(power)

	c.JSON(http.StatusOK, gin.H{
		"gauges": []gin.H{
			{"label": "Hashrate", "value": hashrate, "unit": "TH/s"},
			{"label": "Miner Temp", "value": temperature, "unit": "°C"},
			{"label": "Room Temp", "value": roomTemp, "unit": "°C"},
			{"label": "Power", "value": power, "unit": "W"},
			{"label": "Efficiency", "value": efficiency, "unit": "J/TH"},
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

func environmentHandler(c *gin.Context) {
	data := gin.H{
		"Title": "Mining Dashboard",
	}
	c.HTML(http.StatusOK, "environment.html", data)
}

func minersHandler(c *gin.Context) {
	data := gin.H{
		"Title":    "Mining Dashboard",
		"Machines": machines,
		"Metrics": []gin.H{
			{"Label": "Total Hashrate", "Value": "375.5", "Unit": "MH/s", "Color": "primary"},
			{"Label": "Active Miners", "Value": "3", "Unit": "online", "Color": "success"},
			{"Label": "Total Power", "Value": "2550", "Unit": "W", "Color": "warning"},
			{"Label": "Avg Temperature", "Value": "67", "Unit": "°C", "Color": "danger"},
			{"Label": "Efficiency", "Value": "0.147", "Unit": "MH/W", "Color": "info"},
			{"Label": "Uptime", "Value": "99.8", "Unit": "%", "Color": "secondary"},
		},
	}
	c.HTML(http.StatusOK, "miners.html", data)
}

func manageHandler(c *gin.Context) {
	data := gin.H{
		"Title":    "Mining Dashboard",
		"Machines": machines,
	}
	c.HTML(http.StatusOK, "manage.html", data)
}

func settingsHandler(c *gin.Context) {
	data := gin.H{
		"Title":    "Mining Dashboard",
		"Machines": machines,
	}
	c.HTML(http.StatusOK, "settings.html", data)
}

func powerMiningHandler(c *gin.Context) {
	data := gin.H{
		"Title":    "Mining Dashboard",
		"Machines": machines,
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
	Name string `json:"name" binding:"required"`
	IP   string `json:"ip" binding:"required"`
}

func addMachineHandler(c *gin.Context) {
	var req AddMachineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.AddMachine(req.Name, req.IP); err != nil {
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

	// TODO: Implement actual miner power control via API call to miner
	log.Printf("Setting power to %d%% for miner at %s", req.Power, req.IP)

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

	// TODO: Implement actual miner start via API call to miner
	log.Printf("Starting miner at %s", req.IP)

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

	// TODO: Implement actual miner shutdown via API call to miner
	log.Printf("Shutting down miner at %s", req.IP)

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

	// TODO: Implement actual power control for selected miners
	for _, ip := range req.IPs {
		log.Printf("Setting power to %d%% for miner at %s", req.Power, ip)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"power":   req.Power,
		"ips":     req.IPs,
		"count":   len(req.IPs),
	})
}

func startAllMinersHandler(c *gin.Context) {
	var req BulkMinerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: Implement actual start for selected miners
	for _, ip := range req.IPs {
		log.Printf("Starting miner at %s", ip)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"ips":     req.IPs,
		"count":   len(req.IPs),
	})
}

func shutdownAllMinersHandler(c *gin.Context) {
	var req BulkMinerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: Implement actual shutdown for selected miners
	for _, ip := range req.IPs {
		log.Printf("Shutting down miner at %s", ip)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"ips":     req.IPs,
		"count":   len(req.IPs),
	})
}
