package main

import (
	"errors"
	"flag"
	"log/slog"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"doki-backend/internal/platform/logger"
)

func main() {
	var (
		flagUp      = flag.Bool("up", false, "Apply all pending up migrations")
		flagDown    = flag.Int("down", 0, "Roll back specified number of migration steps (e.g., -down 1)")
		flagVersion = flag.Bool("version", false, "Print current schema migration version and status")
		flagForce   = flag.Int("force", -1, "Force set migration version to recover from dirty state (e.g., -force 2)")
		flagPath    = flag.String("path", "migrations", "Path to migration SQL files directory")
		flagDSN     = flag.String("dsn", "", "PostgreSQL connection DSN (defaults to DATABASE_URL environment variable)")
	)
	flag.Parse()

	log := logger.New(logger.Config{
		Level:  slog.LevelInfo,
		Output: os.Stdout,
	})
	slog.SetDefault(log)

	// Resolve database connection URL
	dsn := *flagDSN
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable"
	}

	// Normalize source path with file:// schema
	sourcePath := *flagPath
	if !strings.HasPrefix(sourcePath, "file://") {
		sourcePath = "file://" + sourcePath
	}

	log.Info("initializing migration runner",
		slog.String("source", sourcePath),
		slog.String("dsn", maskDSN(dsn)),
	)

	m, err := migrate.New(sourcePath, dsn)
	if err != nil {
		log.Error("failed to initialize migration engine", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Warn("error closing migration source", slog.String("error", srcErr.Error()))
		}
		if dbErr != nil {
			log.Warn("error closing migration database", slog.String("error", dbErr.Error()))
		}
	}()

	// Command 1: Inspect Migration Version
	if *flagVersion {
		ver, dirty, err := m.Version()
		if err != nil {
			if errors.Is(err, migrate.ErrNilVersion) {
				log.Info("no migrations applied yet (version 0)")
				return
			}
			log.Error("failed to retrieve migration version", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("current database schema version",
			slog.Uint64("version", uint64(ver)),
			slog.Bool("dirty", dirty),
		)
		if dirty {
			log.Warn("database is in a dirty migration state — manual intervention or -force required")
			os.Exit(1)
		}
		return
	}

	// Command 2: Force Set Version (dirty state recovery)
	if *flagForce >= 0 {
		log.Warn("force setting migration version", slog.Int("force_version", *flagForce))
		if err := m.Force(*flagForce); err != nil {
			log.Error("failed to force migration version", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("successfully forced migration version", slog.Int("version", *flagForce))
		return
	}

	// Command 3: Rollback Migrations (Down Steps)
	if *flagDown > 0 {
		log.Info("executing rollback", slog.Int("steps", *flagDown))
		if err := m.Steps(-(*flagDown)); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				log.Info("no changes detected for rollback")
				return
			}
			log.Error("rollback failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		ver, dirty, _ := m.Version()
		log.Info("rollback completed successfully",
			slog.Uint64("current_version", uint64(ver)),
			slog.Bool("dirty", dirty),
		)
		return
	}

	// Command 4: Apply Up Migrations (Default behavior if -up or no action flags provided)
	if *flagUp || (!*flagVersion && *flagForce < 0 && *flagDown == 0) {
		log.Info("applying pending schema migrations...")
		if err := m.Up(); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				log.Info("schema is already up to date (no changes applied)")
				return
			}
			log.Error("migration execution failed", slog.String("error", err.Error()))
			os.Exit(1)
		}

		ver, dirty, _ := m.Version()
		log.Info("all migrations applied successfully",
			slog.Uint64("current_version", uint64(ver)),
			slog.Bool("dirty", dirty),
		)
	}
}

func maskDSN(dsn string) string {
	if len(dsn) > 24 {
		return dsn[:12] + "..." + dsn[len(dsn)-10:]
	}
	return "***"
}
