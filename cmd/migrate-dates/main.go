package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	var (
		dbPath    = flag.String("db", "subtrackr.db", "Path to SQLite database")
		dryRun    = flag.Bool("dry-run", false, "Show what would be changed without making changes")
		action    = flag.String("action", "compare", "Action to perform: compare, migrate, rollback, stats")
		subID     = flag.Uint("subscription-id", 0, "Subscription ID for single operations")
		reason    = flag.String("reason", "Manual migration", "Reason for migration")
	)
	flag.Parse()

	// Open database
	db, err := gorm.Open(sqlite.Open(*dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Run migrations to ensure schema is up to date
	if err := database.RunMigrations(db); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	// Auto-migrate audit log table
	if err := db.AutoMigrate(&models.DateMigrationLog{}); err != nil {
		log.Fatal("Failed to migrate audit log table:", err)
	}

	// Create migration safety checker
	checker := models.NewDateMigrationSafetyCheck(db)

	switch *action {
	case "compare":
		if *subID == 0 {
			fmt.Println("Comparing all subscriptions V1 vs V2...")
			compareAllSubscriptions(db)
		} else {
			compareSubscription(checker, *subID)
		}

	case "migrate":
		if *subID == 0 {
			fmt.Printf("Migrating all subscriptions to V2 (dry-run: %v)...\n", *dryRun)
			if err := checker.BatchMigrateToV2WithAudit(*dryRun); err != nil {
				log.Fatal("Migration failed:", err)
			}
			fmt.Println("Migration completed successfully")
		} else {
			fmt.Printf("Migrating subscription %d to V2...\n", *subID)
			if err := checker.MigrateSubscriptionToV2(*subID, *reason); err != nil {
				log.Fatal("Migration failed:", err)
			}
			fmt.Println("Subscription migrated successfully")
		}

	case "rollback":
		if *subID == 0 {
			fmt.Println("Batch rollback not supported for safety. Use --subscription-id")
			os.Exit(1)
		}
		fmt.Printf("Rolling back subscription %d to V1...\n", *subID)
		if err := checker.RollbackSubscriptionToV1(*subID, *reason); err != nil {
			log.Fatal("Rollback failed:", err)
		}
		fmt.Println("Subscription rolled back successfully")

	case "stats":
		stats, err := checker.GetMigrationStats()
		if err != nil {
			log.Fatal("Failed to get stats:", err)
		}
		printStats(stats)

	default:
		fmt.Printf("Unknown action: %s\n", *action)
		fmt.Println("Valid actions: compare, migrate, rollback, stats")
		os.Exit(1)
	}
}

func compareAllSubscriptions(db *gorm.DB) {
	var subscriptions []models.Subscription
	db.Find(&subscriptions)

	checker := models.NewDateMigrationSafetyCheck(db)

	fmt.Printf("%-5s %-20s %-12s %-20s %-20s %-10s\n",
		"ID", "Name", "Schedule", "V1 Date", "V2 Date", "Diff (days)")
	fmt.Println(strings.Repeat("-", 90))

	for _, sub := range subscriptions {
		v1Date, v2Date, err := checker.CompareCalculationVersions(sub.ID)
		if err != nil {
			continue
		}

		v1Str := "nil"
		v2Str := "nil"
		diffStr := "N/A"

		if v1Date != nil {
			v1Str = v1Date.Format("2006-01-02")
		}
		if v2Date != nil {
			v2Str = v2Date.Format("2006-01-02")
		}
		if v1Date != nil && v2Date != nil {
			diff := v2Date.Sub(*v1Date).Truncate(24*time.Hour).Hours() / 24
			diffStr = fmt.Sprintf("%.1f", diff)
		}

		name := sub.Name
		if len(name) > 18 {
			name = name[:15] + "..."
		}

		fmt.Printf("%-5d %-20s %-12s %-20s %-20s %-10s\n",
			sub.ID, name, sub.Schedule, v1Str, v2Str, diffStr)
	}
}

func compareSubscription(checker *models.DateMigrationSafetyCheck, id uint) {
	v1Date, v2Date, err := checker.CompareCalculationVersions(id)
	if err != nil {
		log.Fatal("Failed to compare:", err)
	}

	fmt.Printf("Subscription %d comparison:\n", id)
	if v1Date != nil {
		fmt.Printf("V1 Date: %s\n", v1Date.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Println("V1 Date: nil")
	}
	if v2Date != nil {
		fmt.Printf("V2 Date: %s\n", v2Date.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Println("V2 Date: nil")
	}

	if v1Date != nil && v2Date != nil {
		diff := v2Date.Sub(*v1Date).Truncate(24*time.Hour).Hours() / 24
		fmt.Printf("Difference: %.1f days\n", diff)
	}
}

func printStats(stats map[string]interface{}) {
	fmt.Println("Date Calculation Migration Statistics:")
	fmt.Println("=====================================")
	fmt.Printf("V1 Subscriptions: %v\n", stats["v1_subscriptions"])
	fmt.Printf("V2 Subscriptions: %v\n", stats["v2_subscriptions"])
	fmt.Printf("Total Migrations: %v\n", stats["total_migrations"])
	fmt.Printf("Rollbacks: %v\n", stats["rollbacks"])
}