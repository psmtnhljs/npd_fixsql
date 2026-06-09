package main

import (
	"flag"
	"log"
	"time"

	"NodePassDash/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	sqlitePath := flag.String("sqlite", "db/database.db", "SQLite database file path")
	pgDSN := flag.String("pg", "", "PostgreSQL DSN (host=... user=... password=... dbname=... port=... sslmode=...)")
	batchSize := flag.Int("batch", 1000, "Batch size for insertion")
	flag.Parse()

	if *pgDSN == "" {
		log.Fatal("Please provide PostgreSQL DSN: -pg \"host=localhost user=nodepassdash password=xxx dbname=nodepassdash port=5432 sslmode=disable\"")
	}

	// Connect to SQLite
	sqliteDB, err := gorm.Open(sqlite.Open(*sqlitePath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("Failed to connect to SQLite: %v", err)
	}

	// Connect to PostgreSQL
	pgDB, err := gorm.Open(postgres.Open(*pgDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	// Auto-create table structure
	if err := pgDB.AutoMigrate(
		&models.Endpoint{},
		&models.SystemConfig{},
		&models.UserSession{},
		&models.Group{},
		&models.OAuthUser{},
		&models.Tunnel{},
		&models.TunnelOperationLog{},
		&models.TunnelGroup{},
		&models.TrafficHourlySummary{},
		&models.DashboardTrafficSummary{},
		&models.ServiceHistory{},
		&models.Services{},
	); err != nil {
		log.Fatalf("PostgreSQL table creation failed: %v", err)
	}

	// Migrate tables in order (respecting foreign key dependencies)
	tables := []struct {
		name  string
		model interface{}
	}{
		{"endpoints", &[]models.Endpoint{}},
		{"system_configs", &[]models.SystemConfig{}},
		{"user_sessions", &[]models.UserSession{}},
		{"groups", &[]models.Group{}},
		{"oauth_users", &[]models.OAuthUser{}},
		{"tunnels", &[]models.Tunnel{}},
		{"tunnel_operation_logs", &[]models.TunnelOperationLog{}},
		{"tunnel_groups", &[]models.TunnelGroup{}},
		{"traffic_hourly_summary", &[]models.TrafficHourlySummary{}},
		{"dashboard_traffic_summary", &[]models.DashboardTrafficSummary{}},
		{"service_history", &[]models.ServiceHistory{}},
		{"services", &[]models.Services{}},
	}

	for _, t := range tables {
		migrateTable(sqliteDB, pgDB, t.name, t.model, *batchSize)
	}

	log.Println("Data migration complete!")
}

func migrateTable(src, dst *gorm.DB, tableName string, model interface{}, batchSize int) {
	start := time.Now()
	log.Printf("Migrating table: %s ...", tableName)

	// Read all data from SQLite
	if err := src.Table(tableName).Find(model).Error; err != nil {
		log.Printf("  Failed to read %s: %v", tableName, err)
		return
	}

	// Batch insert into PostgreSQL
	if err := dst.Table(tableName).CreateInBatches(model, batchSize).Error; err != nil {
		log.Printf("  Failed to write %s: %v", tableName, err)
		return
	}

	log.Printf("  %s migrated, took: %v", tableName, time.Since(start))
}
