//go:build windows

// config.go — tiny persistent settings (window position, auto-clean
// threshold, deep mode). Stored as plain key=value lines.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds persistent WhalePet settings.
type Config struct {
	X, Y int        // window position
	Auto int        // auto-clean threshold in percent; 0 = disabled
	Deep bool       // deep cleaning mode (purge full standby list)
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	dir = filepath.Join(dir, "WhalePet")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "config.txt")
}

func loadConfig() Config {
	// Auto == -1 means "key absent"; 0 means "explicitly disabled"
	c := Config{Auto: -1}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	present := false
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.ToLower(k))
		v = strings.TrimSpace(v)
		switch k {
		case "x":
			c.X = atoiDefault(v, 0)
		case "y":
			c.Y = atoiDefault(v, 0)
		case "auto":
			c.Auto = atoiDefault(v, -1)
			present = true
		case "deep":
			c.Deep = v == "1" || strings.EqualFold(v, "true")
		}
	}
	if !present {
		c.Auto = -1
	}
	return c
}

func saveConfig(c Config) {
	data := fmt.Sprintf("x=%d\ny=%d\nauto=%d\ndeep=%d\n",
		c.X, c.Y, c.Auto, boolToInt(c.Deep))
	_ = os.WriteFile(configPath(), []byte(data), 0o644)
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
