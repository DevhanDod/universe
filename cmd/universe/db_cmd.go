package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Universe/universe/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

const (
	dbContainerName = "universe_postgres"
	dbImage         = "pgvector/pgvector:pg16"
	dbUser          = "universe_admin"
	dbPassword      = "universe_secret_2024"
	dbName          = "universe"
	dbHostPort      = "5433"
	dbVolume        = "universe_pgdata"
	dbLocalURL      = "postgres://universe_admin:universe_secret_2024@localhost:5433/universe"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database operations",
	Run:   func(cmd *cobra.Command, _ []string) { cmd.Help() },
}

var dbStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Test database connection and show table status",
	RunE:  runDBStatus,
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations from the migrations/ directory",
	RunE:  runDBMigrate,
}

var dbStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the local Postgres + pgvector container via Docker",
	RunE:  runDBStart,
}

var dbStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local Postgres container",
	RunE:  runDBStop,
}

var dbMigratePath string

func init() {
	dbMigrateCmd.Flags().StringVar(&dbMigratePath, "migrations-dir", "migrations", "path to the migrations directory")
	dbCmd.AddCommand(dbStatusCmd, dbMigrateCmd, dbStartCmd, dbStopCmd)
}

// dockerAvailable checks docker CLI presence and daemon reachability.
func dockerAvailable() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found on PATH — install Docker Desktop: https://docs.docker.com/get-docker/")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("docker daemon is not running — start Docker Desktop and try again")
	}
	return nil
}

// containerState returns "running", "stopped", or "" (not found).
func containerState(name string) string {
	out, err := exec.Command("docker", "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.State}}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runDBStart(_ *cobra.Command, _ []string) error {
	if err := dockerAvailable(); err != nil {
		fmt.Printf("❌ %v\n", err)
		return nil
	}

	state := containerState(dbContainerName)
	switch state {
	case "running":
		fmt.Printf("✅ Container %q is already running\n", dbContainerName)
	case "exited", "created", "stopped":
		fmt.Printf("▶️  Starting existing container %q...\n", dbContainerName)
		if out, err := exec.Command("docker", "start", dbContainerName).CombinedOutput(); err != nil {
			return fmt.Errorf("docker start: %w\n%s", err, out)
		}
	default:
		fmt.Printf("📦 Creating container %q (image: %s)...\n", dbContainerName, dbImage)
		args := []string{
			"run", "-d",
			"--name", dbContainerName,
			"-e", "POSTGRES_USER=" + dbUser,
			"-e", "POSTGRES_PASSWORD=" + dbPassword,
			"-e", "POSTGRES_DB=" + dbName,
			"-p", dbHostPort + ":5432",
			"-v", dbVolume + ":/var/lib/postgresql/data",
			"--restart", "unless-stopped",
			dbImage,
		}
		if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("docker run: %w\n%s", err, out)
		}
	}

	// Wait for Postgres to accept connections.
	fmt.Print("⏳ Waiting for Postgres to be ready")
	ready := false
	for i := 0; i < 30; i++ {
		fmt.Print(".")
		if err := exec.Command("docker", "exec", dbContainerName, "pg_isready", "-U", dbUser, "-d", dbName).Run(); err == nil {
			ready = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	fmt.Println()
	if !ready {
		fmt.Println("⚠️  Postgres did not become ready in 30s — check: docker logs " + dbContainerName)
		return nil
	}

	// Persist connection string to config so other commands pick it up automatically.
	cfg := LoadConfig()
	if cfg.DBURL == "" {
		cfg.DBURL = dbLocalURL
		if err := SaveConfig(cfg); err != nil {
			fmt.Printf("⚠️  Failed to save db_url to config: %v\n", err)
		} else {
			fmt.Printf("💾 Saved db_url to %s\n", ConfigFilePath())
		}
	}

	fmt.Println()
	fmt.Println("✅ Database is ready!")
	fmt.Printf("   URL: %s\n", MaskPassword(dbLocalURL))
	fmt.Println()
	fmt.Println("Next: universe db migrate")
	return nil
}

func runDBStop(_ *cobra.Command, _ []string) error {
	if err := dockerAvailable(); err != nil {
		fmt.Printf("❌ %v\n", err)
		return nil
	}
	if containerState(dbContainerName) == "" {
		fmt.Printf("Container %q does not exist — nothing to stop\n", dbContainerName)
		return nil
	}
	fmt.Printf("⏹  Stopping %q...\n", dbContainerName)
	if out, err := exec.Command("docker", "stop", dbContainerName).CombinedOutput(); err != nil {
		return fmt.Errorf("docker stop: %w\n%s", err, out)
	}
	fmt.Println("✅ Stopped")
	return nil
}

func runDBStatus(_ *cobra.Command, _ []string) error {
	dbURL := GetDBURL()
	if dbURL == "" {
		fmt.Println("❌ No database configured")
		fmt.Println()
		fmt.Println("First start PostgreSQL:")
		fmt.Println("   docker compose up -d")
		fmt.Println()
		fmt.Println("Then connect:")
		fmt.Println("   universe config set db postgres://universe_admin:universe_secret_2024@localhost:5433/universe")
		return nil
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
		return nil
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
	fmt.Println()

	// Check tables
	tables := []string{"observations", "skills", "skill_executions", "plans", "plan_costs"}
	for _, table := range tables {
		var count int
		err := conn.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err != nil {
			fmt.Printf("   %-25s ❌ missing (run: universe db migrate)\n", table)
		} else {
			fmt.Printf("   %-25s ✅ %d rows\n", table, count)
		}
	}

	var seedCount int
	_ = conn.QueryRow(ctx, "SELECT COUNT(*) FROM skills WHERE evolution='manual'").Scan(&seedCount)
	fmt.Printf("\n   Seed skills: %d\n", seedCount)

	return nil
}

func runDBMigrate(_ *cobra.Command, _ []string) error {
	dbURL := GetDBURL()
	if dbURL == "" {
		fmt.Println("❌ No database configured")
		fmt.Println("   Run: universe db start")
		return nil
	}

	// If user passed an explicit --migrations-dir, read from disk.
	// Otherwise use the SQL files embedded into the binary at build time, so
	// `universe db migrate` works from any working directory (e.g. a tester's
	// project, not the Universe repo).
	useEmbedded := dbMigratePath == "" || dbMigratePath == "migrations"
	var (
		filesFS  fs.FS
		sourceID string
	)
	if useEmbedded {
		sub, err := fs.Sub(migrations.Files, ".")
		if err != nil {
			return fmt.Errorf("embedded migrations unavailable: %w", err)
		}
		filesFS = sub
		sourceID = "embedded"
	} else {
		filesFS = os.DirFS(filepath.Clean(dbMigratePath))
		sourceID = filepath.Clean(dbMigratePath)
	}

	entries, err := fs.ReadDir(filesFS, ".")
	if err != nil {
		return fmt.Errorf("read migrations (%s): %w", sourceID, err)
	}

	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	sort.Strings(sqlFiles)

	if len(sqlFiles) == 0 {
		fmt.Printf("No .sql files found in %s\n", sourceID)
		return nil
	}

	fmt.Println("🔧 Running database migrations...")
	fmt.Printf("   URL: %s\n", MaskPassword(dbURL))
	fmt.Printf("   Source: %s (%d files)\n", sourceID, len(sqlFiles))
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	for _, name := range sqlFiles {
		sql, err := fs.ReadFile(filesFS, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "   ❌ read %s: %v\n", name, err)
			continue
		}

		fmt.Printf("   Running: %s ... ", name)
		_, err = conn.Exec(ctx, string(sql))
		if err != nil {
			fmt.Printf("❌\n")
			fmt.Fprintf(os.Stderr, "      Error: %v\n", err)
			continue
		}
		fmt.Println("✅")
	}

	fmt.Println()
	fmt.Println("✅ Migrations complete!")
	fmt.Println()
	fmt.Println("Next: universe db status")
	return nil
}
