package main

import (
	"flag"
	"log"
	"net/http"

	"miningRoom/db"

	"github.com/gin-gonic/gin"
)

var (
	machines []db.Machine
	database *db.DB
)

func main() {
	dbPath := flag.String("db-path", "miningroom.db", "SQLite database path")
	questdbHost := flag.String("questdb-host", "localhost", "QuestDB host for metrics")
	questdbPort := flag.Int("questdb-port", 9000, "QuestDB port")
	flag.Parse()

	log.Printf("Using QuestDB at %s:%d", *questdbHost, *questdbPort)

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
	r.GET("/environment", environmentHandler)
	r.GET("/manage", manageHandler)
	r.GET("/settings", settingsHandler)

	// API routes for dashboard data (placeholder for future queries)
	api := r.Group("/api")
	{
		api.GET("/status", getStatusHandler)
		api.GET("/gauges", getGaugesHandler)
		api.GET("/charts", getChartsHandler)

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

func dashboardHandler(c *gin.Context) {
	// Sample data - will be replaced with real queries later
	data := gin.H{
		"Title":    "Mining Dashboard",
		"Machines": machines,
		"Status": gin.H{
			"Online": true,
			"Label":  "System Status",
		},
		"Gauges": []gin.H{
			{"Label": "Hashrate", "Value": 125.5, "Unit": "MH/s"},
			{"Label": "Temperature", "Value": 65, "Unit": "°C"},
			{"Label": "Power", "Value": 850, "Unit": "W"},
			{"Label": "Efficiency", "Value": 0.147, "Unit": "MH/W"},
			{"Label": "Uptime", "Value": 99.8, "Unit": "%"},
		},
	}

	c.HTML(http.StatusOK, "dashboard.html", data)
}

func getStatusHandler(c *gin.Context) {
	// Placeholder for status API
	c.JSON(http.StatusOK, gin.H{
		"online": true,
		"label":  "System Status",
	})
}

func getGaugesHandler(c *gin.Context) {
	// Placeholder for gauges API
	c.JSON(http.StatusOK, gin.H{
		"gauges": []gin.H{
			{"label": "Hashrate", "value": 125.5, "unit": "MH/s"},
			{"label": "Temperature", "value": 65, "unit": "°C"},
			{"label": "Power", "value": 850, "unit": "W"},
			{"label": "Efficiency", "value": 0.147, "unit": "MH/W"},
			{"label": "Uptime", "value": 99.8, "unit": "%"},
		},
	})
}

func getChartsHandler(c *gin.Context) {
	// Placeholder for charts API
	c.JSON(http.StatusOK, gin.H{
		"message": "Chart data will be provided via queries",
	})
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
