// NiceHash API poller for Telegraf exec input.
//
// Fetches mining payouts, rig status, and account balance from the NiceHash API v2,
// then outputs InfluxDB line protocol for Telegraf to forward to QuestDB.
//
// Usage:
//
//	nicehash-telegraf --config /path/to/nicehash_config.json
//
// Build:
//
//	go build -o nicehash-telegraf ./nicehash/
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// toFloat converts a json value that may be a number or a quoted string to float64.
func toFloat(v json.Number) float64 {
	f, err := v.Float64()
	if err != nil {
		// Might be empty string
		f, _ = strconv.ParseFloat(strings.Trim(string(v), `"`), 64)
	}
	return f
}

const baseURL = "https://api2.nicehash.com"

type config struct {
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
	OrgID     string `json:"org_id"`
}

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("reading config: %w", err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.APIKey == "" {
		return config{}, fmt.Errorf("'api_key' missing in config file")
	}
	if cfg.APISecret == "" {
		return config{}, fmt.Errorf("'api_secret' missing in config file")
	}
	if cfg.OrgID == "" {
		return config{}, fmt.Errorf("'org_id' missing in config file")
	}
	return cfg, nil
}

func nicehashRequest(cfg config, method, path, query string) (json.RawMessage, error) {
	xtime := fmt.Sprintf("%d", time.Now().UnixMilli())
	xnonce := uuid.NewString()

	// Build HMAC input: key \0 time \0 nonce \0 \0 org_id \0 \0 method \0 path \0 query
	var msg []byte
	msg = append(msg, []byte(cfg.APIKey)...)
	msg = append(msg, 0)
	msg = append(msg, []byte(xtime)...)
	msg = append(msg, 0)
	msg = append(msg, []byte(xnonce)...)
	msg = append(msg, 0, 0)
	msg = append(msg, []byte(cfg.OrgID)...)
	msg = append(msg, 0, 0)
	msg = append(msg, []byte(method)...)
	msg = append(msg, 0)
	msg = append(msg, []byte(path)...)
	msg = append(msg, 0)
	msg = append(msg, []byte(query)...)

	mac := hmac.New(sha256.New, []byte(cfg.APISecret))
	mac.Write(msg)
	digest := hex.EncodeToString(mac.Sum(nil))

	url := baseURL + path
	if query != "" {
		url += "?" + query
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Time", xtime)
	req.Header.Set("X-Nonce", xnonce)
	req.Header.Set("X-Auth", cfg.APIKey+":"+digest)
	req.Header.Set("X-Organization-Id", cfg.OrgID)
	req.Header.Set("X-Request-Id", uuid.NewString())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.RawMessage(body), nil
}

// escapeTag escapes special characters in InfluxDB tag values.
func escapeTag(value string) string {
	value = strings.ReplaceAll(value, " ", "\\ ")
	value = strings.ReplaceAll(value, ",", "\\,")
	value = strings.ReplaceAll(value, "=", "\\=")
	return value
}

// escapeFieldStr escapes a string field value for InfluxDB line protocol.
func escapeFieldStr(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func fetchPayouts(cfg config) []string {
	var lines []string

	raw, err := nicehashRequest(cfg, "GET", "/main/api/v2/mining/rigs/payouts", "size=10&page=0")
	if err != nil {
		log.Printf("ERROR fetching payouts: %v", err)
		return nil
	}

	var data struct {
		List []struct {
			ID       string      `json:"id"`
			Amount   json.Number `json:"amount"`
			Fee      json.Number `json:"feeAmount"`
			Currency struct {
				EnumName string `json:"enumName"`
			} `json:"currency"`
			Created int64 `json:"created"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Printf("ERROR parsing payouts: %v", err)
		return nil
	}

	for _, p := range data.List {
		if p.Created == 0 {
			continue
		}
		currency := p.Currency.EnumName
		if currency == "" {
			currency = "BTC"
		}
		tsNs := p.Created * 1_000_000 // ms to ns

		tags := fmt.Sprintf("currency=%s", escapeTag(currency))
		fields := fmt.Sprintf("amount=%g,fee=%g,payout_id=%s", toFloat(p.Amount), toFloat(p.Fee), escapeFieldStr(p.ID))
		lines = append(lines, fmt.Sprintf("nicehash_payouts,%s %s %d", tags, fields, tsNs))
	}

	return lines
}

func fetchRigs(cfg config, groupName string) []string {
	var lines []string

	query := "size=50&page=0"
	if groupName != "" {
		query = fmt.Sprintf("size=50&page=0&path=%s", groupName)
	}

	raw, err := nicehashRequest(cfg, "GET", "/main/api/v2/mining/rigs2", query)
	if err != nil {
		log.Printf("ERROR fetching rigs: %v", err)
		return nil
	}

	var data struct {
		UnpaidAmount        json.Number `json:"unpaidAmount"`
		TotalProfitability  json.Number `json:"totalProfitability"`
		NextPayoutTimestamp interface{} `json:"nextPayoutTimestamp"`
		MiningRigs []struct {
			RigID         string      `json:"rigId"`
			Name          string      `json:"name"`
			MinerStatus   string      `json:"minerStatus"`
			UnpaidAmount  json.Number `json:"unpaidAmount"`
			Profitability json.Number `json:"profitability"`
			Stats         []struct {
				SpeedAccepted json.Number `json:"speedAccepted"`
				SpeedRejected json.Number `json:"speedRejected"`
			} `json:"stats"`
		} `json:"miningRigs"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Printf("ERROR parsing rigs: %v", err)
		return nil
	}

	nowNs := time.Now().UnixNano()

	// Account-level summary
	fields := fmt.Sprintf("unpaid_total=%g,profitability_total=%g", toFloat(data.UnpaidAmount), toFloat(data.TotalProfitability))
	if data.NextPayoutTimestamp != nil {
		fields += fmt.Sprintf(",next_payout_ts=%s", escapeFieldStr(fmt.Sprintf("%v", data.NextPayoutTimestamp)))
	}
	lines = append(lines, fmt.Sprintf("nicehash_account %s %d", fields, nowNs))

	// Per-rig data
	for _, rig := range data.MiningRigs {
		rigID := rig.RigID
		if rigID == "" {
			rigID = "unknown"
		}
		rigName := rig.Name
		if rigName == "" {
			rigName = rigID
		}
		status := rig.MinerStatus
		if status == "" {
			status = "UNKNOWN"
		}

		var speedAccepted, speedRejected float64
		for _, s := range rig.Stats {
			speedAccepted += toFloat(s.SpeedAccepted)
			speedRejected += toFloat(s.SpeedRejected)
		}

		tags := fmt.Sprintf("rig_name=%s,rig_id=%s,status=%s",
			escapeTag(rigName), escapeTag(rigID), escapeTag(status))
		rigFields := fmt.Sprintf("unpaid=%g,profitability=%g,speed_accepted=%g,speed_rejected=%g",
			toFloat(rig.UnpaidAmount), toFloat(rig.Profitability), speedAccepted, speedRejected)
		lines = append(lines, fmt.Sprintf("nicehash_rigs,%s %s %d", tags, rigFields, nowNs))
	}

	return lines
}

func fetchBalance(cfg config) []string {
	var lines []string

	raw, err := nicehashRequest(cfg, "GET", "/main/api/v2/accounting/accounts2/", "")
	if err != nil {
		log.Printf("ERROR fetching balance: %v", err)
		return nil
	}

	nowNs := time.Now().UnixNano()

	// Try "currencies" format first
	var currenciesResp struct {
		Currencies []struct {
			Currency  string      `json:"currency"`
			Available json.Number `json:"available"`
			Pending   json.Number `json:"pending"`
		} `json:"currencies"`
	}
	if err := json.Unmarshal(raw, &currenciesResp); err == nil && len(currenciesResp.Currencies) > 0 {
		for _, acc := range currenciesResp.Currencies {
			avail := toFloat(acc.Available)
			pend := toFloat(acc.Pending)
			total := avail + pend
			if total == 0 {
				continue
			}
			currency := acc.Currency
			if currency == "" {
				currency = "UNKNOWN"
			}
			tags := fmt.Sprintf("currency=%s", escapeTag(currency))
			fields := fmt.Sprintf("available=%g,pending=%g,total=%g", avail, pend, total)
			lines = append(lines, fmt.Sprintf("nicehash_balance,%s %s %d", tags, fields, nowNs))
		}
		return lines
	}

	// Try "total" format
	var totalResp struct {
		Total map[string]struct {
			Available json.Number `json:"available"`
			Pending   json.Number `json:"pending"`
		} `json:"total"`
	}
	if err := json.Unmarshal(raw, &totalResp); err == nil && len(totalResp.Total) > 0 {
		for currency, balances := range totalResp.Total {
			avail := toFloat(balances.Available)
			pend := toFloat(balances.Pending)
			total := avail + pend
			if total == 0 {
				continue
			}
			tags := fmt.Sprintf("currency=%s", escapeTag(currency))
			fields := fmt.Sprintf("available=%g,pending=%g,total=%g", avail, pend, total)
			lines = append(lines, fmt.Sprintf("nicehash_balance,%s %s %d", tags, fields, nowNs))
		}
	}

	return lines
}

func main() {
	exe, _ := os.Executable()
	defaultConfig := filepath.Join(filepath.Dir(exe), "config.json")

	configPath := flag.String("config", defaultConfig, "Path to NiceHash config JSON file")
	groupName := flag.String("group-name", "", "Filter rigs by group name")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	var lines []string
	lines = append(lines, fetchRigs(cfg, *groupName)...)
	lines = append(lines, fetchPayouts(cfg)...)
	lines = append(lines, fetchBalance(cfg)...)

	for _, line := range lines {
		fmt.Println(line)
	}
}
