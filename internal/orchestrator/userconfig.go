package orchestrator

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UserConfig holds the model slot mappings.
// Universe never calls these models — they are advisory hints
// returned inside RoutingDecision so the agent (Cursor) maps
// role names to its own configured models.
type UserConfig struct {
	PremiumModel  string `yaml:"premium"`
	LowCostModel  string `yaml:"low_cost"`
}

const defaultConfigContent = "premium: claude-opus-4-7\nlow_cost: claude-haiku-4-5\n"

// LoadUserConfig reads ~/.universe/config.yaml.
// If the file does not exist it is created with sensible defaults.
// The env var UNIVERSE_CONFIG overrides the default path.
func LoadUserConfig() (*UserConfig, error) {
	path := configPath()

	// create default if missing
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return defaultUserConfig(), nil
		}
		_ = os.WriteFile(path, []byte(defaultConfigContent), 0o644)
		return defaultUserConfig(), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return defaultUserConfig(), nil
	}
	defer f.Close()

	cfg := defaultUserConfig()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "premium":
			if val != "" {
				cfg.PremiumModel = val
			}
		case "low_cost":
			if val != "" {
				cfg.LowCostModel = val
			}
		}
	}
	return cfg, scanner.Err()
}

// configPath returns the resolved path to config.yaml.
func configPath() string {
	if p := os.Getenv("UNIVERSE_CONFIG"); p != "" {
		return filepath.Clean(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".universe/config.yaml"
	}
	return filepath.Join(home, ".universe", "config.yaml")
}

func defaultUserConfig() *UserConfig {
	return &UserConfig{
		PremiumModel: "claude-opus-4-7",
		LowCostModel: "claude-haiku-4-5",
	}
}

// Print writes the current config to stdout in a human-readable format.
func (c *UserConfig) Print() {
	fmt.Printf("Config file: %s\n\n", configPath())
	fmt.Printf("  premium:   %s\n", c.PremiumModel)
	fmt.Printf("  low_cost:  %s\n", c.LowCostModel)
	fmt.Printf("\nThese are advisory hints — Universe never calls these models directly.\n")
	fmt.Printf("Cursor maps the role names (premium / low_cost) to its configured models.\n")
}
