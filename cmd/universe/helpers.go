package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Universe/universe/internal/orchestrator"
)

// ── Config file ───────────────────────────────────────────────────────────────

// UniverseConfig is stored at ~/.universe/config.json.
type UniverseConfig struct {
	DBURL          string                   `json:"db_url"`
	Mode           string                   `json:"mode"` // "local" or "team"
	DeveloperID    string                   `json:"developer_id,omitempty"`
	PremiumModel   orchestrator.ModelConfig `json:"premium_model"`
	ExecutionModel orchestrator.ModelConfig `json:"execution_model"`
}

func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".universe")
}

func ConfigFilePath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

func LoadConfig() UniverseConfig {
	data, err := os.ReadFile(ConfigFilePath())
	if err != nil {
		return UniverseConfig{Mode: "local"}
	}
	var cfg UniverseConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return UniverseConfig{Mode: "local"}
	}
	return cfg
}

func SaveConfig(cfg UniverseConfig) error {
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigFilePath(), data, 0o644)
}

// GetDBURL returns the database URL from environment or config file.
// UNIVERSE_DB_URL → DATABASE_URL → config file.
func GetDBURL() string {
	if v := os.Getenv("UNIVERSE_DB_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return LoadConfig().DBURL
}

// MaskPassword replaces the password segment in a postgres URL for safe display.
func MaskPassword(url string) string {
	// postgres://user:password@host/db → postgres://user:***@host/db
	after, found := strings.CutPrefix(url, "postgres://")
	if !found {
		after, found = strings.CutPrefix(url, "postgresql://")
		if !found {
			return url
		}
	}
	at := strings.Index(after, "@")
	if at < 0 {
		return url
	}
	userinfo := after[:at]
	colon := strings.Index(userinfo, ":")
	if colon < 0 {
		return url
	}
	masked := userinfo[:colon] + ":***"
	scheme := "postgres"
	if strings.HasPrefix(url, "postgresql://") {
		scheme = "postgresql"
	}
	return scheme + "://" + masked + "@" + after[at+1:]
}

// ── Local data dir ────────────────────────────────────────────────────────────

func LocalDataDir() string { return ".universe" }

func EnsureLocalDataDir() error { return os.MkdirAll(LocalDataDir(), 0o755) }

// ── Browser ───────────────────────────────────────────────────────────────────

func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}

// ── Output formatting ─────────────────────────────────────────────────────────

func PrintEngine(number int, name, status, detail string) {
	icon := "✅"
	if status != "Active" {
		icon = "⚠️ "
	}
	fmt.Printf("  Engine %d (%s): %s %s\n", number, name, icon, status)
	if detail != "" {
		fmt.Printf("    %s\n", detail)
	}
}

func PrintSection(title string) {
	fmt.Printf("\n%s\n", title)
	fmt.Println("─────────────────────────────────────────────")
}
