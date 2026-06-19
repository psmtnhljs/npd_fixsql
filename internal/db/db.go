package db

import (
	applog "NodePassDash/internal/log"
	"NodePassDash/internal/models"
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	gormDB *gorm.DB
	once   sync.Once

	dbHealthCtx    context.Context
	dbHealthCancel context.CancelFunc
)

func GetDB() *gorm.DB {
	once.Do(func() {
		config := GetDBConfig()
		var err error

		var logLevel logger.LogLevel
		switch config.LogLevel {
		case "silent":
			logLevel = logger.Silent
		case "error":
			logLevel = logger.Error
		case "warn":
			logLevel = logger.Warn
		case "info":
			logLevel = logger.Info
		default:
			logLevel = logger.Info
		}

		gormConfig := &gorm.Config{
			Logger: logger.Default.LogMode(logLevel),
			NowFunc: func() time.Time {
				return time.Now().Local()
			},
		}

		gormDB, err = gorm.Open(postgres.Open(config.BuildDSN()), gormConfig)
		if err != nil {
			log.Fatalf("连接 PostgreSQL 数据库失败: %v", err)
		}

		sqlDB, err := gormDB.DB()
		if err != nil {
			log.Fatalf("获取 PostgreSQL 底层连接失败: %v", err)
		}

		sqlDB.SetMaxOpenConns(config.MaxOpenConns)
		sqlDB.SetMaxIdleConns(config.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(config.MaxLifetime)
		sqlDB.SetConnMaxIdleTime(config.MaxIdleTime)

		if err := sqlDB.Ping(); err != nil {
			log.Fatalf("PostgreSQL 数据库连接测试失败: %v", err)
		}

		if err := AutoMigrate(gormDB); err != nil {
			log.Fatalf("数据库迁移失败: %v", err)
		}

		config.PrintConfig()
		log.Printf("PostgreSQL 数据库连接成功并完成表结构迁移")

		dbHealthCtx, dbHealthCancel = context.WithCancel(context.Background())
		go startConnectionHealthCheck(dbHealthCtx)
	})
	return gormDB
}

func startConnectionHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("健康检查已停止")
			return
		case <-ticker.C:
		}

		if gormDB == nil {
			continue
		}

		sqlDB, err := gormDB.DB()
		if err != nil {
			log.Printf("健康检查获取 sql.DB 失败: %v", err)
			continue
		}

		if err := sqlDB.Ping(); err != nil {
			log.Printf("健康检查发现数据库连接异常: %v", err)
			if strings.Contains(err.Error(), "database is closed") {
				log.Printf("检测到数据库已关闭，停止健康检查")
				return
			}
		}

		stats := sqlDB.Stats()
		if stats.MaxOpenConnections > 0 && stats.OpenConnections > int(float64(stats.MaxOpenConnections)*0.8) {
			log.Printf("警告：连接池使用率较高 %d/%d", stats.OpenConnections, stats.MaxOpenConnections)
		}
	}
}

func AutoMigrate(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Endpoint{}) {
		return QuickInitSchema(db)
	}

	return StandardMigrate(db)
}

func QuickInitSchema(db *gorm.DB) error {
	log.Println("检测到全新数据库，使用快速初始化模式")

	return db.AutoMigrate(
		&models.Endpoint{},
		&models.SystemConfig{},
		&models.UserSession{},
		&models.Group{},
		&models.OAuthUser{},
		&models.Tunnel{},
		&models.TunnelOperationLog{},
		&models.TunnelGroup{},
		&models.EndpointSSE{},
		&models.TrafficHourlySummary{},
		&models.DashboardTrafficSummary{},
		&models.ServiceHistory{},
		&models.Services{},
	)
}

func StandardMigrate(db *gorm.DB) error {
	log.Println("检测到现有数据库，使用标准迁移模式")

	return db.AutoMigrate(
		&models.Endpoint{},
		&models.SystemConfig{},
		&models.UserSession{},
		&models.Group{},
		&models.OAuthUser{},
		&models.Tunnel{},
		&models.TunnelOperationLog{},
		&models.TunnelGroup{},
		&models.EndpointSSE{},
		&models.TrafficHourlySummary{},
		&models.DashboardTrafficSummary{},
		&models.ServiceHistory{},
		&models.Services{},
	)
}

func Close() error {
	if gormDB != nil {
		if dbHealthCancel != nil {
			dbHealthCancel()
		}
		sqlDB, err := gormDB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

func ExecuteWithRetry(fn func(*gorm.DB) error) error {
	maxRetries := 3
	baseDelay := 100 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		db := GetHealthyDB()
		err := fn(db)
		if err == nil {
			return nil
		}

		if isRetryableError(err) && i < maxRetries-1 {
			delay := time.Duration(1<<uint(i)) * baseDelay
			log.Printf("数据库操作失败，%v 后重试（第 %d 次）: %v", delay, i+1, err)
			time.Sleep(delay)
			continue
		}

		return err
	}
	return nil
}

func TxWithRetry(fn func(*gorm.DB) error) error {
	maxRetries := 3
	baseDelay := 100 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		db := GetHealthyDB()
		err := db.Transaction(fn)
		if err == nil {
			return nil
		}

		if isRetryableError(err) && i < maxRetries-1 {
			delay := time.Duration(1<<uint(i)) * baseDelay
			log.Printf("数据库事务失败，%v 后重试（第 %d 次）: %v", delay, i+1, err)
			time.Sleep(delay)
			continue
		}

		return err
	}
	return nil
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "connection refused") ||
		contains(errStr, "connection reset") ||
		contains(errStr, "broken pipe") ||
		contains(errStr, "server closed the connection unexpectedly") ||
		contains(errStr, "too many clients") ||
		contains(errStr, "deadlock detected") ||
		contains(errStr, "could not serialize access") ||
		contains(errStr, "EOF")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func PingDB() error {
	db := GetDB()
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func GetHealthyDB() *gorm.DB {
	db := GetDB()

	sqlDB, err := db.DB()
	if err != nil {
		log.Printf("获取 sql.DB 失败，重新初始化连接: %v", err)
		once = sync.Once{}
		return GetDB()
	}

	if err := sqlDB.Ping(); err != nil {
		log.Printf("数据库连接异常，重新初始化连接: %v", err)
		once = sync.Once{}
		return GetDB()
	}

	return db
}

func DB() interface{} {
	return GetDB()
}

func InitSchema() error {
	return AutoMigrate(GetDB())
}

func UpdateEndpointTunnelCount(endpointID int64) {
	go func() {
		time.Sleep(50 * time.Millisecond)

		err := ExecuteWithRetry(func(db *gorm.DB) error {
			return db.Model(&models.Endpoint{}).Where("id = ?", endpointID).
				Update("tunnel_count", db.Model(&models.Tunnel{}).Where("endpoint_id = ?", endpointID).Count(nil)).Error
		})

		if err != nil {
			applog.Errorf("[DB]更新端点 %d 隧道计数失败: %v", endpointID, err)
		} else {
			applog.Debugf("[DB]端点 %d 隧道计数已更新", endpointID)
		}
	}()
}

func UpdateEndpointTunnelCountSync(endpointID int64) error {
	return ExecuteWithRetry(func(db *gorm.DB) error {
		return db.Model(&models.Endpoint{}).Where("id = ?", endpointID).
			Update("tunnel_count", db.Model(&models.Tunnel{}).Where("endpoint_id = ?", endpointID).Count(nil)).Error
	})
}
