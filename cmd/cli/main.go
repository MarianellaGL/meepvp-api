package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path"
	"time"

	"tablescore-api/auth"
	"tablescore-api/config"
	"tablescore-api/models"
	"tablescore-api/services"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

// migrationsDir is where `migrate create` writes new files, relative to the
// repository root. The server never reads it; migrations are embedded.
const migrationsDir = "migrations"

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	slog.SetDefault(config.SetupLogger(cfg.IsProd(), cfg.Log.Level))

	// migrate create only writes a file; do it before touching the database.
	if len(os.Args) >= 3 && os.Args[1] == "migrate" && os.Args[2] == "create" {
		runMigrateCreate(os.Args[3:])
		return
	}

	db, err := models.InitDB(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to get underlying sql.DB: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	slog.Info("connected to database", "dbname", cfg.DB.DBName)

	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}

	args := os.Args[2:]
	switch os.Args[1] {
	case "migrate":
		runMigrate(db, args)
	case "ping":
		runPing(sqlDB)
	case "adduser":
		runAddUser(db, args)
	case "rmuser":
		runRmUser(db, args)
	case "resetdb":
		runResetDB(cfg, db, args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("MeppVP CLI")
	fmt.Println("Usage: cli <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  migrate   Manage schema migrations: up (default) | down | status | create <name>")
	fmt.Println("  ping      Test database connection")
	fmt.Println("  adduser   Create a new user")
	fmt.Println("  rmuser    Remove a user by email")
	fmt.Println("  resetdb   Roll back all migrations and re-apply them")
}

func runMigrate(db *gorm.DB, args []string) {
	sub := "up"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "up":
		if err := models.Migrate(db); err != nil {
			log.Fatalf("migration failed: %v", err)
		}
		slog.Info("migrations up to date")
	case "down":
		version, err := models.MigrateDown(db)
		if errors.Is(err, goose.ErrNoNextVersion) {
			fmt.Println("Nothing to roll back.")
			return
		}
		if err != nil {
			log.Fatalf("rollback failed: %v", err)
		}
		fmt.Printf("Rolled back migration %d\n", version)
	case "status":
		statuses, err := models.MigrationStatus(db)
		if err != nil {
			log.Fatalf("status failed: %v", err)
		}
		for _, s := range statuses {
			applied := "pending"
			if s.State == goose.StateApplied {
				applied = s.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Printf("%-24s %s\n", applied, path.Base(s.Source.Path))
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown migrate subcommand: %s\n", sub)
		os.Exit(1)
	}
}

// runMigrateCreate writes the next numbered SQL migration file. It never
// touches the database, so main dispatches it before models.InitDB.
func runMigrateCreate(args []string) {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "Error: migrate create takes exactly one name, e.g. migrate create add_widgets")
		os.Exit(1)
	}
	if _, err := os.Stat(migrationsDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s not found; run from the repository root\n", migrationsDir)
		os.Exit(1)
	}
	goose.SetSequential(true)
	if err := goose.Create(nil, migrationsDir, args[0], "sql"); err != nil {
		log.Fatalf("create failed: %v", err)
	}
}

func runPing(sqlDB *sql.DB) {
	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}
	slog.Info("database connection is healthy")
}

func runAddUser(db *gorm.DB, args []string) {
	fs := flag.NewFlagSet("adduser", flag.ExitOnError)
	email := fs.String("email", "", "User email (required)")
	name := fs.String("name", "", "User display name (required)")
	password := fs.String("password", "", "User password (required)")
	admin := fs.Bool("admin", false, "Grant admin role")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *email == "" || *name == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "Error: --email, --name, and --password are required")
		fs.Usage()
		os.Exit(1)
	}
	// Same rules as the register and admin-create endpoints.
	if msg := auth.ValidatePasswordComplexity(*password); msg != "" {
		fmt.Fprintln(os.Stderr, "Error:", msg)
		os.Exit(1)
	}

	user, err := services.CreateEmailUser(db, *email, *name, *password)
	if err != nil {
		log.Fatalf("failed to create user: %v", err)
	}
	// The operator vouches for the address, so skip the emailed link.
	if err := services.MarkEmailVerified(db, user.ID); err != nil {
		log.Fatalf("failed to mark email verified: %v", err)
	}

	role := auth.RoleUser
	if *admin {
		if _, err := services.SetUserRole(db, user.ID, auth.RoleAdmin); err != nil {
			log.Fatalf("failed to set admin role: %v", err)
		}
		role = auth.RoleAdmin
	}

	fmt.Printf("User created: %s (%s) [%s]\n", user.Email, user.Name, role)
}

func runRmUser(db *gorm.DB, args []string) {
	fs := flag.NewFlagSet("rmuser", flag.ExitOnError)
	email := fs.String("email", "", "Email of the user to remove (required)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *email == "" {
		fmt.Fprintln(os.Stderr, "Error: --email is required")
		fs.Usage()
		os.Exit(1)
	}

	if err := services.DeleteUserByEmail(db, *email); err != nil {
		log.Fatalf("failed to remove user: %v", err)
	}

	fmt.Printf("User removed: %s\n", *email)
}

func runResetDB(cfg *config.Config, db *gorm.DB, args []string) {
	fs := flag.NewFlagSet("resetdb", flag.ExitOnError)
	confirm := fs.Bool("yes", false, "Skip confirmation prompt")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if !*confirm {
		fmt.Printf("This will roll back every migration in %q and re-apply them. Continue? [y/N] ", cfg.DB.DBName)
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil || (answer != "y" && answer != "Y") {
			fmt.Println("Aborted.")
			os.Exit(0)
		}
	}

	if err := models.Reset(db); err != nil {
		log.Fatalf("failed to reset database: %v", err)
	}

	fmt.Println("Database reset and migrations applied successfully.")
}
