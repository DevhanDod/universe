# Universe CLI — Cobra Command Wiring

## Build Specification for Claude Code

**Name:** Wire all CLI commands into the Go binary using cobra  
**Purpose:** Replace the shell script POC with a real compiled Go CLI  
**Library:** `github.com/spf13/cobra` (industry standard Go CLI framework)  
**Estimated effort:** 1 day  
**Dependencies:** All 5 engines built. MCP server built. Dashboard built.  

---

## 1. What This Does (Plain English)

Right now the `universe` command is a shell script that fakes the output. This spec turns it into the real Go binary where every command actually calls the real engine code.

After building this, when a developer types `universe init`, it actually scans the codebase with tree-sitter. When they type `universe status`, it actually queries PostgreSQL for real engine stats. When they type `universe mcp --stdio`, it actually starts the MCP server that Cursor connects to.

This is the final assembly step — we built all the engine parts, now we're connecting the steering wheel to the actual engine.

---

## 2. Project Structure

```
cmd/
└── universe/
    ├── main.go           # Entry point + root command + version flag
    ├── init.go           # universe init — scan codebase, build graph
    ├── status.go         # universe status — show all engine stats
    ├── mcp.go            # universe mcp --stdio — start MCP server
    ├── dashboard.go      # universe dashboard — start dashboard web server
    ├── config.go         # universe config set/get/reset — database config
    ├── db.go             # universe db status/migrate — database operations
    ├── skills.go         # universe skills list/lineage/freeze/unfreeze
    ├── graph.go          # universe graph export — export graph data
    └── helpers.go        # Shared helpers: getDBURL, openBrowser, printEngine
```

---

## 3. Go Module Dependency

```bash
go get github.com/spf13/cobra@latest
```

---

## 4. File: `cmd/universe/main.go`

The entry point. Sets up the root command and global flags.

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

// Version is set at build time via ldflags:
//   go build -ldflags "-X main.Version=0.1.0"
var Version = "dev"

// Root command — runs when user types just "universe" with no subcommand
var rootCmd = &cobra.Command{
    Use:   "universe",
    Short: "AI-powered code intelligence for developer agents",
    Long: `Universe — 5 engines that make your AI agent smarter, faster, and cheaper.

  Engine 1: Knowledge Graph    — cross-repo dependency mapping
  Engine 2: Persistent Memory  — agent remembers across sessions
  Engine 3: Self-Evolving Skills — recipes that improve over time
  Engine 4: Compression        — 75% fewer output tokens
  Engine 5: Orchestrator       — premium plans, free executes

Get started:
  universe init          Scan your codebase
  universe status        Check all engines
  universe mcp --stdio   Start MCP server for Cursor`,

    // When user types just "universe" with no args, show help
    Run: func(cmd *cobra.Command, args []string) {
        cmd.Help()
    },
}

// Global flag: --version
var versionFlag bool

func init() {
    rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Print version")
    rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
        if versionFlag {
            fmt.Printf("universe v%s\n", Version)
            os.Exit(0)
        }
    }
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

---

## 5. File: `cmd/universe/helpers.go`

Shared utilities used by multiple commands.

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
)

// ============================================================
// CONFIG FILE
// ============================================================

// ConfigDir returns the path to ~/.universe/
func ConfigDir() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".universe")
}

// ConfigFilePath returns the path to ~/.universe/config.json
func ConfigFilePath() string {
    return filepath.Join(ConfigDir(), "config.json")
}

// UniverseConfig represents the config.json structure
type UniverseConfig struct {
    DBURL string `json:"db_url"`
    Mode  string `json:"mode"` // "local" or "team"
}

// LoadConfig reads ~/.universe/config.json
// Returns default config if file doesn't exist
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

// SaveConfig writes ~/.universe/config.json
func SaveConfig(cfg UniverseConfig) error {
    if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
        return err
    }
    data, err := json.MarshalIndent(cfg, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(ConfigFilePath(), data, 0644)
}

// GetDBURL returns the database URL from config or environment variable.
// Environment variable takes priority.
func GetDBURL() string {
    if envURL := os.Getenv("UNIVERSE_DB_URL"); envURL != "" {
        return envURL
    }
    cfg := LoadConfig()
    return cfg.DBURL
}

// MaskPassword replaces the password in a PostgreSQL URL for display.
// "postgres://user:secret@host:5432/db" → "postgres://user:***@host:5432/db"
func MaskPassword(url string) string {
    // Find :// then mask between : and @
    // Simple approach: regex or string manipulation
    // Implementation left to Claude Code — straightforward string operation
    return url // placeholder
}

// ============================================================
// LOCAL DATA DIR
// ============================================================

// LocalDataDir returns the path to .universe/ in the current project directory
func LocalDataDir() string {
    return ".universe"
}

// EnsureLocalDataDir creates .universe/ in the current directory if it doesn't exist
func EnsureLocalDataDir() error {
    return os.MkdirAll(LocalDataDir(), 0755)
}

// ============================================================
// BROWSER
// ============================================================

// OpenBrowser opens the given URL in the default browser
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

// ============================================================
// OUTPUT FORMATTING
// ============================================================

// PrintEngine prints an engine status line with color
// number: 1-5
// name: "Knowledge Graph"
// status: "Active" or "Unavailable"
// detail: "142 nodes, 287 edges"
func PrintEngine(number int, name string, status string, detail string) {
    icon := "✅"
    if status != "Active" {
        icon = "⚠️ "
    }
    fmt.Printf("  Engine %d (%s): %s %s\n", number, name, icon, status)
    if detail != "" {
        fmt.Printf("    %s\n", detail)
    }
}

// PrintSection prints a section header
func PrintSection(title string) {
    fmt.Printf("\n%s\n", title)
    fmt.Println("─────────────────────────────────────────────")
}
```

---

## 6. File: `cmd/universe/init.go`

`universe init` — scans the codebase, builds the knowledge graph, stores it locally.

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"
    "time"

    "github.com/spf13/cobra"

    "universe/internal/analyzer"
    "universe/internal/graph"
    "universe/internal/scanner"
)

var initCmd = &cobra.Command{
    Use:   "init",
    Short: "Scan codebase and build the knowledge graph",
    Long: `Scans all source files in the current directory using tree-sitter.
Parses functions, types, imports, and builds a dependency graph.
Stores the graph locally in .universe/ directory.

If a database is configured, also syncs the graph to PostgreSQL
for team-wide access.`,
    Run: runInit,
}

// Flags
var initPath string

func init() {
    initCmd.Flags().StringVarP(&initPath, "path", "p", ".", "Path to scan (default: current directory)")
    rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) {
    startTime := time.Now()

    // Resolve path
    absPath, err := filepath.Abs(initPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
        os.Exit(1)
    }

    fmt.Println("🔍 Scanning codebase...")
    fmt.Printf("   Path: %s\n", absPath)

    dbURL := GetDBURL()
    if dbURL != "" {
        fmt.Println("   Mode: team (PostgreSQL)")
    } else {
        fmt.Println("   Mode: local (SQLite)")
    }
    fmt.Println()

    // Step 1: Scan files
    // Use your existing scanner.Scan() function from internal/scanner/
    fmt.Print("   Scanning files...")
    files, err := scanner.Scan(absPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "\n   Error scanning: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf(" found %d source files\n", len(files))

    // Step 2: Parse and extract
    // Use your existing analyzer.Analyze() function
    fmt.Print("   Parsing with tree-sitter...")
    result, err := analyzer.Analyze(absPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "\n   Error parsing: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf(" %d functions, %d types, %d packages\n",
        result.FunctionCount, result.TypeCount, result.PackageCount)

    // Step 3: Build graph
    fmt.Print("   Building knowledge graph...")
    g := result.Graph
    fmt.Printf(" %d nodes, %d edges\n", g.NodeCount(), g.EdgeCount())

    // Step 4: Store locally
    if err := EnsureLocalDataDir(); err != nil {
        fmt.Fprintf(os.Stderr, "   Error creating .universe dir: %v\n", err)
        os.Exit(1)
    }

    localPath := filepath.Join(LocalDataDir(), "graph.json")
    fmt.Printf("   Stored locally: %s\n", localPath)
    if err := g.SaveJSON(localPath); err != nil {
        fmt.Fprintf(os.Stderr, "   Error saving graph: %v\n", err)
        os.Exit(1)
    }

    // Step 5: Sync to PostgreSQL if configured
    if dbURL != "" {
        fmt.Print("   Syncing to PostgreSQL...")
        // Call graph.SyncToPostgres(dbURL, g)
        // This would insert/update graph nodes and edges in the DB
        fmt.Println(" ✓")
    }

    elapsed := time.Since(startTime).Round(time.Millisecond)
    fmt.Println()
    fmt.Println("✅ Graph ready!")
    fmt.Printf("   Time: %s\n", elapsed)
    fmt.Printf("   Nodes: %d | Edges: %d\n", g.NodeCount(), g.EdgeCount())

    // Step 6: Detect cross-repo if multiple repos found
    repos := g.UniqueRepos()
    if len(repos) > 1 {
        crossEdges := g.CrossRepoEdgeCount()
        fmt.Printf("   Repos: %d | Cross-repo edges: %d\n", len(repos), crossEdges)
    }

    fmt.Println()
    fmt.Println("Next steps:")
    fmt.Println("   universe status        Check all engines")
    fmt.Println("   universe mcp --stdio   Start MCP server for Cursor")
    fmt.Println("   universe dashboard     Open the dashboard")
}
```

---

## 7. File: `cmd/universe/status.go`

`universe status` — shows the status of all 5 engines with real data.

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "universe/internal/graph"
    "universe/internal/memory"
    "universe/internal/skills"
)

var statusCmd = &cobra.Command{
    Use:   "status",
    Short: "Show the status of all 5 engines",
    Run:   runStatus,
}

func init() {
    rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()

    fmt.Println("🌌 Universe Status")
    fmt.Println("═══════════════════════════════════════════════")

    // ── Engine 1: Knowledge Graph ──
    // Load from local .universe/graph.json
    g, err := graph.LoadJSON(".universe/graph.json")
    if err != nil {
        PrintEngine(1, "Knowledge Graph", "Unavailable", "Run 'universe init' to scan your codebase")
    } else {
        repos := g.UniqueRepos()
        detail := fmt.Sprintf("%d nodes, %d edges", g.NodeCount(), g.EdgeCount())
        if len(repos) > 1 {
            detail += fmt.Sprintf(", %d repos, %d cross-repo edges", len(repos), g.CrossRepoEdgeCount())
        }
        PrintEngine(1, "Knowledge Graph", "Active", detail)
    }

    // ── Engines 2-5 need database ──
    if dbURL == "" {
        PrintEngine(2, "Persistent Memory", "Unavailable", "Connect a database: universe config set db postgres://...")
        PrintEngine(3, "Evolving Skills", "Unavailable", "Connect a database: universe config set db postgres://...")
        PrintEngine(4, "Compression", "Active", "compact mode (no database needed)")
        PrintEngine(5, "Orchestrator", "Unavailable", "Connect a database: universe config set db postgres://...")
        printStatusFooter("local", "")
        return
    }

    // ── Engine 2: Memory ──
    memStore, err := memory.NewStore(dbURL)
    if err != nil {
        PrintEngine(2, "Persistent Memory", "Error", err.Error())
    } else {
        defer memStore.Close()
        stats, err := memStore.GetStats()
        if err != nil {
            PrintEngine(2, "Persistent Memory", "Error", err.Error())
        } else {
            recallRate := 0.0
            if stats.TotalObservations > 0 && stats.TotalRecalls > 0 {
                recallRate = float64(stats.TotalRecalls) / float64(stats.TotalObservations) * 100
            }
            detail := fmt.Sprintf("%d observations, %.0f%% recall rate, %d shared",
                stats.TotalObservations, recallRate, stats.SharedObservations)
            PrintEngine(2, "Persistent Memory", "Active", detail)
        }
    }

    // ── Engine 3: Skills ──
    skillStore, err := skills.NewStore(dbURL)
    if err != nil {
        PrintEngine(3, "Evolving Skills", "Error", err.Error())
    } else {
        defer skillStore.Close()
        stats, err := skillStore.GetStats()
        if err != nil {
            PrintEngine(3, "Evolving Skills", "Error", err.Error())
        } else {
            detail := fmt.Sprintf("%d active, %d frozen, avg %.0f%% success",
                stats.TotalActive, stats.TotalFrozen, stats.AvgSuccessRate*100)
            PrintEngine(3, "Evolving Skills", "Active", detail)
        }
    }

    // ── Engine 4: Compression ──
    // Compression is always active — it's just prompt building, no database
    PrintEngine(4, "Compression", "Active", "compact mode, graph shorthand enabled")

    // ── Engine 5: Orchestrator ──
    // Query the cost tracking table for today's stats
    // Use a direct DB query since the orchestrator may not be running as a service
    costStats, err := queryTodayCostStats(dbURL)
    if err != nil {
        PrintEngine(5, "Orchestrator", "Active", "cost tracking ready (no tasks today)")
    } else {
        detail := fmt.Sprintf("%.0f%% Haiku, $%.2f today, %d tasks",
            costStats.HaikuPct, costStats.CostToday, costStats.TasksToday)
        PrintEngine(5, "Orchestrator", "Active", detail)
    }

    printStatusFooter("team", dbURL)
}

func printStatusFooter(mode string, dbURL string) {
    fmt.Println("═══════════════════════════════════════════════")
    if mode == "local" {
        fmt.Println("  Mode:     local (SQLite)")
        fmt.Println("  Database: not connected")
    } else {
        fmt.Printf("  Mode:     team (PostgreSQL)\n")
        fmt.Printf("  Database: %s\n", MaskPassword(dbURL))
    }
    fmt.Printf("  Platform: %s\n", getPlatform())
    fmt.Printf("  Version:  %s\n", Version)
}

// queryTodayCostStats queries agent_costs table for today's summary
type TodayCostStats struct {
    TasksToday int
    CostToday  float64
    HaikuPct   float64
}

func queryTodayCostStats(dbURL string) (*TodayCostStats, error) {
    // Direct PostgreSQL query:
    //   SELECT
    //     COUNT(DISTINCT task_id) as tasks,
    //     SUM(cost_usd) as cost,
    //     COUNT(*) FILTER (WHERE model='haiku')::float / GREATEST(COUNT(*),1) * 100 as haiku_pct
    //   FROM agent_costs
    //   WHERE created_at >= CURRENT_DATE
    //
    // Implementation: use pgx to query directly
    return nil, fmt.Errorf("no data")
}

func getPlatform() string {
    return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}
```

---

## 8. File: `cmd/universe/config.go`

`universe config set/get/reset` — database configuration.

```go
package main

import (
    "fmt"
    "os"
    "strings"

    "github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
    Use:   "config",
    Short: "Manage Universe configuration",
    Run: func(cmd *cobra.Command, args []string) {
        cmd.Help()
    },
}

var configSetCmd = &cobra.Command{
    Use:   "set [key] [value]",
    Short: "Set a configuration value",
    Long:  "Set a configuration value. Currently supports: db",
    Args:  cobra.ExactArgs(2),
    Run:   runConfigSet,
}

var configGetCmd = &cobra.Command{
    Use:   "get [key]",
    Short: "Get a configuration value",
    Args:  cobra.ExactArgs(1),
    Run:   runConfigGet,
}

var configResetCmd = &cobra.Command{
    Use:   "reset",
    Short: "Reset configuration to defaults (local SQLite mode)",
    Run:   runConfigReset,
}

func init() {
    configCmd.AddCommand(configSetCmd)
    configCmd.AddCommand(configGetCmd)
    configCmd.AddCommand(configResetCmd)
    rootCmd.AddCommand(configCmd)
}

func runConfigSet(cmd *cobra.Command, args []string) {
    key := args[0]
    value := args[1]

    switch key {
    case "db":
        // Validate PostgreSQL URL format
        if !strings.HasPrefix(value, "postgres://") && !strings.HasPrefix(value, "postgresql://") {
            fmt.Fprintln(os.Stderr, "❌ Invalid database URL")
            fmt.Fprintln(os.Stderr, "")
            fmt.Fprintln(os.Stderr, "Expected format:")
            fmt.Fprintln(os.Stderr, "   postgres://user:password@host:port/database")
            fmt.Fprintln(os.Stderr, "")
            fmt.Fprintln(os.Stderr, "Example:")
            fmt.Fprintln(os.Stderr, "   universe config set db postgres://universe_admin:universe_secret_2024@localhost:5432/universe")
            os.Exit(1)
        }

        cfg := LoadConfig()
        cfg.DBURL = value
        cfg.Mode = "team"
        if err := SaveConfig(cfg); err != nil {
            fmt.Fprintf(os.Stderr, "❌ Error saving config: %v\n", err)
            os.Exit(1)
        }

        fmt.Println("✅ Database URL saved")
        fmt.Printf("   URL: %s\n", MaskPassword(value))
        fmt.Printf("   Config: %s\n", ConfigFilePath())
        fmt.Println()
        fmt.Println("Next steps:")
        fmt.Println("   universe db status      Test the connection")
        fmt.Println("   universe db migrate     Run migrations (first time)")

    default:
        fmt.Fprintf(os.Stderr, "Unknown config key: %s\n", key)
        fmt.Fprintln(os.Stderr, "Supported keys: db")
        os.Exit(1)
    }
}

func runConfigGet(cmd *cobra.Command, args []string) {
    key := args[0]

    switch key {
    case "db":
        dbURL := GetDBURL()
        if dbURL != "" {
            fmt.Printf("Database URL: %s\n", MaskPassword(dbURL))
            fmt.Printf("Config file:  %s\n", ConfigFilePath())
        } else {
            fmt.Println("Database: local SQLite (no PostgreSQL configured)")
            fmt.Println()
            fmt.Println("To connect:")
            fmt.Println("   universe config set db postgres://user:pass@host:5432/universe")
        }
    default:
        fmt.Fprintf(os.Stderr, "Unknown config key: %s\n", key)
        os.Exit(1)
    }
}

func runConfigReset(cmd *cobra.Command, args []string) {
    cfg := UniverseConfig{Mode: "local"}
    if err := SaveConfig(cfg); err != nil {
        fmt.Fprintf(os.Stderr, "❌ Error saving config: %v\n", err)
        os.Exit(1)
    }
    fmt.Println("✅ Reset to local SQLite mode")
    fmt.Printf("   Config: %s\n", ConfigFilePath())
}
```

---

## 9. File: `cmd/universe/db.go`

`universe db status/migrate` — database operations.

```go
package main

import (
    "context"
    "embed"
    "fmt"
    "os"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/spf13/cobra"
)

//go:embed ../../migrations/*.sql
var migrationFiles embed.FS

var dbCmd = &cobra.Command{
    Use:   "db",
    Short: "Database operations",
    Run: func(cmd *cobra.Command, args []string) {
        cmd.Help()
    },
}

var dbStatusCmd = &cobra.Command{
    Use:   "status",
    Short: "Test database connection and show table status",
    Run:   runDBStatus,
}

var dbMigrateCmd = &cobra.Command{
    Use:   "migrate",
    Short: "Run database migrations",
    Run:   runDBMigrate,
}

func init() {
    dbCmd.AddCommand(dbStatusCmd)
    dbCmd.AddCommand(dbMigrateCmd)
    rootCmd.AddCommand(dbCmd)
}

func runDBStatus(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("❌ No database configured")
        fmt.Println()
        fmt.Println("First, start PostgreSQL:")
        fmt.Println("   docker compose up -d")
        fmt.Println()
        fmt.Println("Then connect:")
        fmt.Println("   universe config set db postgres://universe_admin:universe_secret_2024@localhost:5432/universe")
        os.Exit(1)
    }

    fmt.Println("🔍 Testing database connection...")
    fmt.Printf("   URL: %s\n", MaskPassword(dbURL))
    fmt.Println()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    conn, err := pgx.Connect(ctx, dbURL)
    if err != nil {
        fmt.Println("❌ Connection failed!")
        fmt.Printf("   Error: %v\n", err)
        fmt.Println()
        fmt.Println("Check that:")
        fmt.Println("   1. Docker is running:  docker compose ps")
        fmt.Println("   2. URL is correct:     universe config get db")
        os.Exit(1)
    }
    defer conn.Close(ctx)

    fmt.Println("✅ Connection successful!")
    fmt.Println()

    // Check pgvector
    var vectorVersion string
    err = conn.QueryRow(ctx, "SELECT extversion FROM pg_extension WHERE extname='vector'").Scan(&vectorVersion)
    if err != nil {
        fmt.Println("   pgvector: not installed")
    } else {
        fmt.Printf("   pgvector: v%s\n", vectorVersion)
    }

    // Check tables
    tables := []string{"observations", "skills", "skill_executions", "agent_costs"}
    for _, table := range tables {
        var count int
        err := conn.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
        if err != nil {
            fmt.Printf("   %-20s ❌ missing\n", table)
        } else {
            fmt.Printf("   %-20s ✅ %d rows\n", table, count)
        }
    }

    // Check seed skills
    var seedCount int
    conn.QueryRow(ctx, "SELECT COUNT(*) FROM skills WHERE evolution='manual'").Scan(&seedCount)
    fmt.Printf("\n   Seed skills: %d\n", seedCount)
}

func runDBMigrate(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("❌ No database configured")
        fmt.Println("   Run: universe config set db postgres://...")
        os.Exit(1)
    }

    fmt.Println("🔧 Running database migrations...")
    fmt.Printf("   URL: %s\n", MaskPassword(dbURL))
    fmt.Println()

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    conn, err := pgx.Connect(ctx, dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "❌ Connection failed: %v\n", err)
        os.Exit(1)
    }
    defer conn.Close(ctx)

    // Read and execute each migration file from embedded FS
    entries, err := migrationFiles.ReadDir("migrations")
    if err != nil {
        fmt.Fprintf(os.Stderr, "❌ Error reading migrations: %v\n", err)
        os.Exit(1)
    }

    for _, entry := range entries {
        if entry.IsDir() {
            continue
        }
        fmt.Printf("   Running: %s\n", entry.Name())

        sql, err := migrationFiles.ReadFile("migrations/" + entry.Name())
        if err != nil {
            fmt.Fprintf(os.Stderr, "   ❌ Error reading %s: %v\n", entry.Name(), err)
            continue
        }

        _, err = conn.Exec(ctx, string(sql))
        if err != nil {
            fmt.Fprintf(os.Stderr, "   ❌ Error in %s: %v\n", entry.Name(), err)
            continue
        }
        fmt.Printf("   ✅ %s applied\n", entry.Name())
    }

    fmt.Println()
    fmt.Println("✅ Migrations complete!")
    fmt.Println()
    fmt.Println("Next: universe db status")
}
```

---

## 10. File: `cmd/universe/mcp.go`

`universe mcp --stdio` — starts the MCP server for Cursor.

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"

    "github.com/spf13/cobra"

    "universe/internal/graph"
    "universe/internal/mcpserver"
    "universe/internal/memory"
    "universe/internal/skills"
)

var mcpCmd = &cobra.Command{
    Use:   "mcp",
    Short: "Start MCP server for AI agent integration (Cursor, Claude Code)",
    Long: `Starts the Universe MCP server over stdio.
Cursor and Claude Code connect to this server to access
the knowledge graph, memory, skills, and all other tools.

Add to ~/.cursor/mcp.json:
  {
    "mcpServers": {
      "universe": {
        "command": "universe",
        "args": ["mcp", "--stdio"]
      }
    }
  }`,
    Run: runMCP,
}

var mcpStdio bool

func init() {
    mcpCmd.Flags().BoolVar(&mcpStdio, "stdio", true, "Use stdio transport (stdin/stdout)")
    rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) {
    // Load graph (required)
    g, err := graph.LoadJSON(".universe/graph.json")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: graph not found. Run 'universe init' first.\n")
        os.Exit(1)
    }

    // Build server config — engines that need DB may be nil (graceful degradation)
    config := mcpserver.ServerConfig{
        Version: Version,
        Graph:   g,
    }

    // Try to connect engines that need database
    dbURL := GetDBURL()
    if dbURL != "" {
        // Engine 2: Memory
        if memStore, err := memory.NewStore(dbURL); err == nil {
            config.MemoryStore = memStore
            // Initialize retriever and session manager
            // (embedder and graph querier setup here)
            config.Retriever = memory.NewRetriever(memStore, nil, nil, memory.DefaultConfig())
            config.SessionMgr = memory.NewSessionManager(memStore, nil, nil, memory.DefaultConfig())
        } else {
            fmt.Fprintf(os.Stderr, "Warning: memory engine unavailable: %v\n", err)
        }

        // Engine 3: Skills
        if skillStore, err := skills.NewStore(dbURL); err == nil {
            config.SkillStore = skillStore
            config.SkillMatcher = skills.NewMatcher(skillStore, nil, skills.DefaultConfig())
            config.SkillExec = skills.NewExecutor(skillStore, skills.DefaultConfig())
        } else {
            fmt.Fprintf(os.Stderr, "Warning: skills engine unavailable: %v\n", err)
        }
    }

    // Handle graceful shutdown
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    // Start MCP server — blocks until client disconnects
    fmt.Fprintf(os.Stderr, "Universe MCP server starting (stdio, v%s)...\n", Version)
    if err := mcpserver.RunStdio(ctx, config); err != nil {
        fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
        os.Exit(1)
    }
}
```

---

## 11. File: `cmd/universe/dashboard.go`

`universe dashboard` — starts the dashboard web server.

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "universe/internal/dashboard"
)

var dashboardCmd = &cobra.Command{
    Use:   "dashboard",
    Short: "Open the Universe dashboard (per-engine views)",
    Long: `Starts the dashboard web server showing all 5 engine views:
  - Overview: headline cost savings and engine status
  - Graph: interactive dependency visualization with badges
  - Memory: observation stream with filters
  - Skills: evolution trees and success rates
  - Compression: before/after prompt comparison
  - Routing: task flight tracker with cost per task`,
    Run: runDashboard,
}

var dashboardPort int
var dashboardNoOpen bool

func init() {
    dashboardCmd.Flags().IntVar(&dashboardPort, "port", 3001, "Dashboard port")
    dashboardCmd.Flags().BoolVar(&dashboardNoOpen, "no-open", false, "Don't auto-open browser")
    rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("⚠️  No database configured — dashboard will show limited data.")
        fmt.Println("   Connect: universe config set db postgres://...")
        fmt.Println()
    }

    server, err := dashboard.NewServer(dbURL, dashboardPort)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error starting dashboard: %v\n", err)
        os.Exit(1)
    }

    url := fmt.Sprintf("http://localhost:%d", dashboardPort)
    fmt.Printf("📊 Universe Dashboard running at %s\n", url)
    fmt.Println("   Press Ctrl+C to stop")
    fmt.Println()

    if !dashboardNoOpen {
        if err := OpenBrowser(url); err != nil {
            fmt.Fprintf(os.Stderr, "   Could not open browser: %v\n", err)
            fmt.Printf("   Open manually: %s\n", url)
        }
    }

    // Blocks until Ctrl+C
    if err := server.Start(); err != nil {
        fmt.Fprintf(os.Stderr, "Dashboard error: %v\n", err)
        os.Exit(1)
    }
}
```

---

## 12. File: `cmd/universe/skills.go`

`universe skills list/lineage/freeze/unfreeze` — skill management.

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "universe/internal/skills"
)

var skillsCmd = &cobra.Command{
    Use:   "skills",
    Short: "Manage self-evolving skills",
    Run: func(cmd *cobra.Command, args []string) {
        cmd.Help()
    },
}

var skillsListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all active skills",
    Run:   runSkillsList,
}

var skillsLineageCmd = &cobra.Command{
    Use:   "lineage [skill-name]",
    Short: "Show the evolution history of a skill",
    Args:  cobra.ExactArgs(1),
    Run:   runSkillsLineage,
}

var skillsFreezeCmd = &cobra.Command{
    Use:   "freeze [skill-id]",
    Short: "Freeze a skill (stop it from evolving)",
    Args:  cobra.ExactArgs(1),
    Run:   runSkillsFreeze,
}

var skillsUnfreezeCmd = &cobra.Command{
    Use:   "unfreeze [skill-id]",
    Short: "Unfreeze a skill (allow it to evolve again)",
    Args:  cobra.ExactArgs(1),
    Run:   runSkillsUnfreeze,
}

func init() {
    skillsCmd.AddCommand(skillsListCmd)
    skillsCmd.AddCommand(skillsLineageCmd)
    skillsCmd.AddCommand(skillsFreezeCmd)
    skillsCmd.AddCommand(skillsUnfreezeCmd)
    rootCmd.AddCommand(skillsCmd)
}

func runSkillsList(cmd *cobra.Command, args []string) {
    dbURL := GetDBURL()
    if dbURL == "" {
        fmt.Println("❌ No database configured. Skills need a database.")
        fmt.Println("   Run: universe config set db postgres://...")
        os.Exit(1)
    }

    store, err := skills.NewStore(dbURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
        os.Exit(1)
    }
    defer store.Close()

    stats, err := store.GetStats()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }

    fmt.Println("📋 Active Skills")
    fmt.Println("═══════════════════════════════════════════════")
    // Query and display all active skills
    // Format: name, version, evolution, language, success rate, confidence, applied count
    // ...
    fmt.Printf("\n  %d active, %d frozen\n", stats.TotalActive, stats.TotalFrozen)
}

func runSkillsLineage(cmd *cobra.Command, args []string) {
    // Query skill by name, get lineage via recursive CTE
    // Display as: v1 (captured) → v2 (fix) → v3 (fix) [active]
    //                    ↳ variant-python v1 (derived)
}

func runSkillsFreeze(cmd *cobra.Command, args []string) {
    // Call store.FreezeSkill(args[0])
    fmt.Printf("🧊 Skill %s frozen. It will continue working (old version) but stop evolving.\n", args[0])
}

func runSkillsUnfreeze(cmd *cobra.Command, args []string) {
    // Call store.UnfreezeSkill(args[0])
    fmt.Printf("🔥 Skill %s unfrozen. Evolution re-enabled.\n", args[0])
}
```

---

## 13. File: `cmd/universe/graph.go`

`universe graph export` — export graph data.

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "universe/internal/graph"
)

var graphCmd = &cobra.Command{
    Use:   "graph",
    Short: "Graph operations",
    Run: func(cmd *cobra.Command, args []string) {
        cmd.Help()
    },
}

var graphExportCmd = &cobra.Command{
    Use:   "export",
    Short: "Export the knowledge graph as JSON",
    Run:   runGraphExport,
}

var exportFormat string
var exportOutput string

func init() {
    graphExportCmd.Flags().StringVar(&exportFormat, "format", "json", "Export format: json")
    graphExportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file (default: stdout)")
    graphCmd.AddCommand(graphExportCmd)
    rootCmd.AddCommand(graphCmd)
}

func runGraphExport(cmd *cobra.Command, args []string) {
    g, err := graph.LoadJSON(".universe/graph.json")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: graph not found. Run 'universe init' first.\n")
        os.Exit(1)
    }

    if exportOutput != "" {
        if err := g.SaveJSON(exportOutput); err != nil {
            fmt.Fprintf(os.Stderr, "Error saving: %v\n", err)
            os.Exit(1)
        }
        fmt.Printf("Exported graph to %s (%d nodes, %d edges)\n", exportOutput, g.NodeCount(), g.EdgeCount())
    } else {
        // Print to stdout
        data, _ := g.ToJSON()
        fmt.Println(string(data))
    }
}
```

---

## 14. Complete Command Tree

After building all files, the CLI supports:

```
universe                              Show help and all available commands
universe --version                    Print version (e.g., "universe v0.1.0")

universe init                         Scan codebase, build graph
universe init --path /other/project   Scan a specific directory

universe status                       Show all 5 engine statuses with real data

universe mcp --stdio                  Start MCP server (Cursor/Claude Code connect here)

universe dashboard                    Start dashboard at localhost:3001
universe dashboard --port 4000        Custom port
universe dashboard --no-open          Don't auto-open browser

universe config set db <url>          Save PostgreSQL connection URL
universe config get db                Show current database URL (password masked)
universe config reset                 Reset to local SQLite mode

universe db status                    Test database connection, show table counts
universe db migrate                   Run all SQL migrations

universe skills list                  List all active skills with stats
universe skills lineage <name>        Show skill evolution tree
universe skills freeze <id>           Stop a skill from evolving
universe skills unfreeze <id>         Re-enable skill evolution

universe graph export                 Export graph as JSON to stdout
universe graph export -o graph.json   Export graph to file
```

---

## 15. Testing

```go
package main

import "testing"

// Test 1: Root command shows help without error
func TestRootCmd(t *testing.T) {
    // Execute rootCmd with no args
    // Verify no error, output contains "universe"
}

// Test 2: --version flag prints version
func TestVersionFlag(t *testing.T) {
    // Execute with --version
    // Verify output contains "universe v"
}

// Test 3: Config set/get round-trip
func TestConfigSetGet(t *testing.T) {
    // Set db URL
    // Get db URL → verify it matches
    // Reset → verify empty
}

// Test 4: Config set rejects invalid URL
func TestConfigSetInvalidURL(t *testing.T) {
    // Try to set "not-a-postgres-url"
    // Verify error message
}

// Test 5: GetDBURL prefers environment variable over config
func TestGetDBURL_EnvPriority(t *testing.T) {
    // Set config to URL-A
    // Set env UNIVERSE_DB_URL to URL-B
    // Verify GetDBURL returns URL-B
}

// Test 6: MaskPassword masks correctly
func TestMaskPassword(t *testing.T) {
    // Input: "postgres://user:secret123@host:5432/db"
    // Expected: "postgres://user:***@host:5432/db"
}

// Test 7: Status runs without database (shows unavailable)
func TestStatus_NoDB(t *testing.T) {
    // Run status with no DB configured
    // Verify Engine 1 shows based on local graph
    // Verify Engine 2-5 show "Unavailable" with helpful message
}

// Test 8: Init fails gracefully with empty directory
func TestInit_EmptyDir(t *testing.T) {
    // Run init in a directory with no source files
    // Should complete with 0 files found, not crash
}

// Test 9: All subcommands are registered
func TestSubcommandRegistration(t *testing.T) {
    // Verify rootCmd has: init, status, mcp, dashboard, config, db, skills, graph
    // Verify config has: set, get, reset
    // Verify db has: status, migrate
    // Verify skills has: list, lineage, freeze, unfreeze
    // Verify graph has: export
}

// Test 10: DB migrate embeds migration files
func TestMigrationsEmbedded(t *testing.T) {
    // Read migrationFiles embed.FS
    // Verify at least one .sql file exists
}
```

---

## 16. Acceptance Criteria

- [ ] `go build -o universe ./cmd/universe` compiles without errors
- [ ] `./universe` shows help with all commands listed
- [ ] `./universe --version` prints version
- [ ] `./universe init` scans a Go project and creates `.universe/graph.json`
- [ ] `./universe init` reports file count, node count, and edge count
- [ ] `./universe status` shows all 5 engines with real data when DB is connected
- [ ] `./universe status` shows graceful "unavailable" when no DB is configured
- [ ] `./universe config set db <url>` saves to `~/.universe/config.json`
- [ ] `./universe config get db` shows URL with masked password
- [ ] `./universe config reset` clears the database URL
- [ ] `UNIVERSE_DB_URL` environment variable overrides config file
- [ ] `./universe db status` connects to PostgreSQL and shows table counts
- [ ] `./universe db migrate` runs embedded SQL migrations
- [ ] `./universe mcp --stdio` starts MCP server that Cursor can connect to
- [ ] `./universe mcp --stdio` works with graph only (no DB — graceful degradation)
- [ ] `./universe dashboard` starts web server and opens browser
- [ ] `./universe skills list` shows skills from the database
- [ ] `./universe graph export` outputs JSON to stdout
- [ ] All 10 tests pass
- [ ] No panics on any command with missing data/config

---

## 17. What We Expect After Building This

After Claude Code builds this spec, you will have:

**A single Go binary** that replaces the shell script POC. Every command actually works — `universe init` actually scans code, `universe status` actually queries the database, `universe mcp --stdio` actually starts a real MCP server that Cursor can connect to.

**The developer experience becomes real:**
```bash
# This now actually works end-to-end:
universe init                    # real tree-sitter scanning
universe config set db postgres://...  # saves real config
universe db migrate              # creates real tables
universe status                  # shows real engine stats
universe mcp --stdio             # real MCP server, Cursor connects
universe dashboard               # real web dashboard with real data
universe skills list             # real skills from the database
```

**You can test the Cursor connection:**
1. Build: `go build -o universe ./cmd/universe`
2. Scan: `./universe init`
3. Create `~/.cursor/mcp.json` pointing to `universe mcp --stdio`
4. Restart Cursor
5. Ask: "What depends on ValidateToken?" — Cursor calls the real tool, gets real graph data

**This is the moment** the system goes from "spec files and demos" to "a real tool that works." After this, the only step left is npm packaging (`npm-setup.md`) to distribute it.
