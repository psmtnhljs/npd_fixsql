# Database Memory Fix & PostgreSQL Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the memory growth issues in the current SQLite database layer and migrate the entire project from SQLite to PostgreSQL, applying the same fixes in the PG context.

**Architecture:** Two-phase approach — Phase 1 fixes critical SQLite bugs (service_history cleanup, in-memory map TTL, missing indexes, table name drift) and makes them immediately effective. Phase 2 replaces the SQLite backend with PostgreSQL by swapping drivers, rewriting all raw SQL, updating Docker/deployment configs, and adding a one-time data migration path.

**Tech Stack:** Go 1.23, GORM, PostgreSQL (pgx driver), Docker, SQLite (legacy, removed at end)

---

## Phase 1: SQLite Memory & Cleanup Fixes

These fixes are independent and should be done first. They solve the immediate memory growth problem.

---

### Task 1: Fix service_history cleanup — integrate into scheduler

**Problem:** `CleanOldTrafficData()` at `internal/dashboard/traffic_service.go:290` deletes service_history >7 days, but this method is never called by the scheduler. The scheduler at `internal/dashboard/scheduler.go:111` calls `ExecuteFullCleanup()` which cleans SSE/summaries/logs but NOT service_history.

**Files:**
- Modify: `internal/dashboard/cleanup_service.go:67` (add service_history cleanup step)

- [ ] **Step 1: Add service_history cleanup to ExecuteFullCleanup**

In `internal/dashboard/cleanup_service.go`, add a new method and call it from `ExecuteFullCleanup()`:

```go
// cleanupServiceHistory 清理过期的service_history数据
func (s *CleanupService) cleanupServiceHistory() CleanupResult {
	start := time.Now()
	result := CleanupResult{
		TableName: "service_history",
		Duration:  0,
	}

	cutoffTime := time.Now().AddDate(0, 0, -7) // 7天保留期

	totalDeleted := int64(0)
	batchSize := s.config.BatchSize

	for {
		var deletedCount int64
		err := s.db.Exec(`
			DELETE FROM service_history
			WHERE id IN (
				SELECT id FROM service_history
				WHERE record_time < ?
				LIMIT ?
			)
		`, cutoffTime, batchSize).Error

		if err != nil {
			result.Error = fmt.Errorf("删除service_history数据失败: %v", err)
			break
		}

		deletedCount = s.db.RowsAffected
		totalDeleted += deletedCount

		if deletedCount < int64(batchSize) {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	result.DeletedCount = totalDeleted
	result.Duration = time.Since(start)
	return result
}
```

Then in `ExecuteFullCleanup()`, add between the SSE cleanup and summary cleanup (after line 79):

```go
// 1.5 清理过期的service_history数据
historyResult := s.cleanupServiceHistory()
if historyResult.Error != nil {
	log.Printf("[数据清理] service_history清理失败: %v", historyResult.Error)
} else {
	log.Printf("[数据清理] service_history清理完成: 删除 %d 条记录，耗时 %v", historyResult.DeletedCount, historyResult.Duration)
}
results = append(results, historyResult)
```

- [ ] **Step 2: Add WAL checkpoint after cleanup**

In `optimizeTables()` at line 260, replace the VACUUM-only approach with WAL checkpoint + VACUUM:

```go
func (s *CleanupService) optimizeTables() CleanupResult {
	start := time.Now()
	result := CleanupResult{
		TableName: "database_optimization",
		Duration:  0,
	}

	// 先执行 WAL checkpoint 将 WAL 文件内容合并回主数据库
	if err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		log.Printf("[数据清理] WAL checkpoint 失败: %v", err)
		result.Error = fmt.Errorf("WAL checkpoint 失败: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	log.Println("[数据清理] WAL checkpoint(TRUNCATE) 成功")

	// 然后执行 VACUUM 回收空间
	if err := s.db.Exec("VACUUM").Error; err != nil {
		log.Printf("[数据清理] VACUUM 优化失败: %v", err)
		result.Error = fmt.Errorf("VACUUM 优化失败: %v", err)
	} else {
		log.Println("[数据清理] VACUUM 优化成功")
	}

	result.Duration = time.Since(start)
	return result
}
```

- [ ] **Step 3: Verify the fix compiles**

Run: `go build ./...`
Expected: PASS

---

### Task 2: Fix table name drift — endpoint_sse vs endpoint_sse_events

**Problem:** Model defines table as `endpoint_sse_events` (`internal/models/models.go:304`) but cleanup code deletes from `endpoint_sse` (`internal/dashboard/cleanup_service.go:113,130-137,289`). Also in `traffic_service.go:295`.

**Files:**
- Modify: `internal/dashboard/cleanup_service.go:113-161` (fix table name)
- Modify: `internal/dashboard/cleanup_service.go:289` (fix stats query)
- Modify: `internal/dashboard/traffic_service.go:295` (fix CleanOldTrafficData)

- [ ] **Step 1: Fix cleanup_service.go SSE cleanup**

In `cleanupSSEData()`, change all references from `endpoint_sse` to `endpoint_sse_events`:

```go
func (s *CleanupService) cleanupSSEData() CleanupResult {
	start := time.Now()
	result := CleanupResult{
		TableName: "endpoint_sse_events",
		Duration:  0,
	}

	cutoffTime := time.Now().AddDate(0, 0, -s.config.SSEDataRetentionDays)

	totalDeleted := int64(0)
	batchSize := s.config.BatchSize

	for {
		var deletedCount int64
		err := s.db.Exec(`
			DELETE FROM endpoint_sse_events
			WHERE id IN (
				SELECT id FROM endpoint_sse_events
				WHERE event_time < ?
				LIMIT ?
			)
		`, cutoffTime, batchSize).Error

		if err != nil {
			result.Error = fmt.Errorf("删除SSE数据失败: %v", err)
			break
		}

		deletedCount = s.db.RowsAffected
		totalDeleted += deletedCount

		if deletedCount < int64(batchSize) {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	result.DeletedCount = totalDeleted
	result.Duration = time.Since(start)
	return result
}
```

- [ ] **Step 2: Fix GetCleanupStats SSE queries**

In `GetCleanupStats()` at line 289, change `endpoint_sse` to `endpoint_sse_events`:

```go
s.db.Raw("SELECT COUNT(*) FROM endpoint_sse_events").Scan(&sseCount)
s.db.Raw("SELECT COUNT(*) FROM endpoint_sse_events WHERE event_time < ?", cutoffTime).Scan(&sseOldCount)
```

- [ ] **Step 3: Fix traffic_service.go CleanOldTrafficData**

In `CleanOldTrafficData()` at line 295, change `endpoint_sse` to `endpoint_sse_events`:

```go
if err := tx.Exec(`
	DELETE FROM endpoint_sse_events
	WHERE event_time < datetime('now', '-30 days')
	AND event_type IN ('initial', 'update')
`).Error; err != nil {
```

Note: also fix `push_type` → `event_type` (the column name per the model).

- [ ] **Step 4: Verify**

Run: `go build ./...`
Expected: PASS

---

### Task 3: Add composite index on service_history

**Problem:** The trend API at `internal/api/tunnel_metrics.go:409` queries `WHERE instance_id = ? AND record_time >= ? ORDER BY record_time ASC`. There's only a single-column index on `instance_id` and a single-column index on `record_time`. A composite index would be much more efficient.

**Files:**
- Modify: `internal/models/models.go:248-280` (add composite index tag)

- [ ] **Step 1: Add composite index to ServiceHistory model**

In `internal/models/models.go`, update the `ServiceHistory` struct to add a composite index:

```go
type ServiceHistory struct {
	ID         int64  `json:"id" gorm:"primaryKey;autoIncrement;column:id"`
	EndpointID int64  `json:"endpointId" gorm:"not null;index;column:endpoint_id"`
	InstanceID string `json:"instanceId" gorm:"type:text;not null;index:idx_instance_record_time;column:instance_id"`

	// ... all other fields unchanged ...

	RecordTime time.Time `json:"recordTime" gorm:"not null;index:idx_instance_record_time;column:record_time"`
	CreatedAt  time.Time `json:"createdAt" gorm:"autoCreateTime;column:created_at"`

	// ...
}
```

This creates a composite index `idx_instance_record_time` on `(instance_id, record_time)` which matches the query pattern.

- [ ] **Step 2: Add manual index creation for existing databases**

In `internal/db/db.go`, after `AutoMigrate()` call (around line 103), add:

```go
// 创建复合索引（如果不存在）
if err := createPerformanceIndexes(gormDB); err != nil {
	log.Printf("创建性能索引失败: %v", err)
}
```

Add new function:

```go
// createPerformanceIndexes 创建性能优化索引
func createPerformanceIndexes(db *gorm.DB) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_instance_record_time ON service_history (instance_id, record_time)",
	}

	for _, idx := range indexes {
		if err := db.Exec(idx).Error; err != nil {
			return fmt.Errorf("创建索引失败 [%s]: %v", idx, err)
		}
	}
	return nil
}
```

- [ ] **Step 3: Verify**

Run: `go build ./...`
Expected: PASS

---

### Task 4: TTL/eviction for HistoryWorker.serviceCurrentStatusData

**Problem:** `serviceCurrentStatusData` map at `internal/sse/history_worker.go:58` only grows — keys are added for each new instance but never removed when instances go offline or are deleted.

**Files:**
- Modify: `internal/sse/history_worker.go` (add TTL, idle cleanup, explicit delete)

- [ ] **Step 1: Add lastAccess field to ServiceCurrentStatus**

```go
type ServiceCurrentStatus struct {
	Result     []MonitoringData
	mu         sync.RWMutex
	lastAccess time.Time // 最后访问时间
}
```

- [ ] **Step 2: Update lastAccess on every data point**

In `processMonitoringData()` at line 172, after `currentStatus.mu.Lock()`:

```go
currentStatus.mu.Lock()
currentStatus.lastAccess = time.Now()
```

- [ ] **Step 3: Add idle cleanup goroutine in NewHistoryWorker**

Add a new goroutine that runs every 5 minutes, removes entries idle for >10 minutes:

```go
func NewHistoryWorker(db *gorm.DB) *HistoryWorker {
	worker := &HistoryWorker{
		db:                       db,
		serviceCurrentStatusData: make(map[string]*ServiceCurrentStatus),
		dataInputChan:            make(chan MonitoringData, 15000),
		stopChan:                 make(chan struct{}),
	}

	worker.wg.Add(1)
	go worker.dataProcessWorker()

	// 启动空闲数据清理协程
	worker.wg.Add(1)
	go worker.idleCleanupWorker()

	log.Info("历史数据处理Worker已启动")
	return worker
}
```

Add the cleanup worker:

```go
// idleCleanupWorker 定期清理长时间无数据更新的实例状态
func (hw *HistoryWorker) idleCleanupWorker() {
	defer hw.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	const maxIdleTime = 10 * time.Minute

	for {
		select {
		case <-hw.stopChan:
			return
		case <-ticker.C:
			hw.cleanupIdleEntries(maxIdleTime)
		}
	}
}

func (hw *HistoryWorker) cleanupIdleEntries(maxIdle time.Duration) {
	now := time.Now()
	hw.mu.Lock()
	defer hw.mu.Unlock()

	for key, status := range hw.serviceCurrentStatusData {
		status.mu.RLock()
		idle := now.Sub(status.lastAccess)
		status.mu.RUnlock()

		if idle > maxIdle {
			delete(hw.serviceCurrentStatusData, key)
			log.Debugf("[HistoryWorker]清理空闲实例状态: %s (空闲%v)", key, idle)
		}
	}
}
```

- [ ] **Step 4: Add explicit RemoveInstance method**

For use when instances are deleted or go offline:

```go
// RemoveInstance 显式移除指定实例的状态数据（实例下线或删除时调用）
func (hw *HistoryWorker) RemoveInstance(endpointID int64, instanceID string) {
	key := hw.buildDataKey(endpointID, instanceID)
	hw.mu.Lock()
	delete(hw.serviceCurrentStatusData, key)
	hw.mu.Unlock()
	log.Debugf("[HistoryWorker]显式移除实例状态: %s", key)
}
```

- [ ] **Step 5: Wire RemoveInstance into SSE delete/shutdown events**

In `internal/sse/service.go`, in `handleDeleteEvent()` and `handleShutdownEvent()`, call:

```go
hw.historyWorker.RemoveInstance(endpointID, instanceID)
```

- [ ] **Step 6: Verify**

Run: `go build ./...`
Expected: PASS

---

### Task 5: Make temp_store configurable and tighten connection pool

**Problem:** `temp_store(memory)` at `internal/db/config.go:160` forces all SQLite temp tables into RAM. With heavy writes this increases RSS. Connection pool defaults (10 open, 5 idle) may also be too aggressive for SQLite.

**Files:**
- Modify: `internal/db/config.go` (add TempStore config option, adjust defaults)

- [ ] **Step 1: Add TempStore to DBConfig**

```go
type DBConfig struct {
	Database     string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
	MaxIdleTime  time.Duration
	LogLevel     string
	WALMode      bool
	TempStore    string // "default", "file", "memory"
}
```

Set default: `TempStore: "memory"` (keep current behavior by default, but allow override).

- [ ] **Step 2: Add env/flag support for TempStore**

In `loadFromEnv()`:

```go
if value := os.Getenv("DB_TEMP_STORE"); value != "" {
	if value == "default" || value == "file" || value == "memory" {
		config.TempStore = value
	}
}
```

- [ ] **Step 3: Use TempStore in BuildDSN**

```go
func (c *DBConfig) BuildDSN() string {
	dsn := c.Database + "?_pragma=foreign_keys(1)"

	if c.WALMode {
		dsn += "&_pragma=journal_mode(WAL)"
	}

	dsn += "&_pragma=busy_timeout(30000)"
	dsn += "&_pragma=synchronous(NORMAL)"
	dsn += "&_pragma=cache_size(2000)"
	dsn += "&_pragma=temp_store(" + c.TempStore + ")"

	return dsn
}
```

- [ ] **Step 4: Adjust default connection pool to more conservative values**

```go
config := DBConfig{
	Database:     dbDir + "/database.db",
	MaxOpenConns: 5,  // 收紧：SQLite 单写特性下 5 足够
	MaxIdleConns: 3,  // 收紧
	MaxLifetime:  5 * time.Minute,
	MaxIdleTime:  2 * time.Minute,
	LogLevel:     "silent",
	WALMode:      true,
	TempStore:    "memory",
}
```

- [ ] **Step 5: Verify**

Run: `go build ./...`
Expected: PASS

---

### Task 6: Schedule auth cache and metrics cleanup

**Problem:** `sessionCache`/`oauthStateCache` at `internal/auth/service.go:273,601` have cleanup functions but no scheduler. `taskStatuses` at `internal/metrics/aggregator.go:532` and `trafficSnapshots` at `internal/metrics/sse_processor.go:166` also lack cleanup scheduling.

**Files:**
- Modify: `internal/dashboard/scheduler.go` (add auth + metrics cleanup tasks)
- Modify: `internal/auth/service.go` (export cleanup methods if needed)

- [ ] **Step 1: Add auth cache cleanup to scheduler**

In `internal/dashboard/scheduler.go`, add a periodic task that calls `authService.CleanupExpiredSessions()` and clears stale oauth states.

Since the scheduler doesn't have access to authService, we need to pass it in or use a callback pattern. The simplest approach: add a `CleanupCallbacks` field:

```go
type TrafficScheduler struct {
	db               *gorm.DB
	trafficService   *TrafficService
	cleanupService   *CleanupService
	ticker           *time.Ticker
	ctx              context.Context
	cancel           context.CancelFunc
	extraCleanupFns  []func() // 额外的清理回调
}

// RegisterCleanupCallback 注册额外的清理回调函数
func (s *TrafficScheduler) RegisterCleanupCallback(fn func()) {
	s.extraCleanupFns = append(s.extraCleanupFns, fn)
}
```

In `executeCleanup()`, after the main cleanup:

```go
// 执行额外的清理回调
for _, fn := range s.extraCleanupFns {
	fn()
}
```

- [ ] **Step 2: Wire auth cleanup in main.go**

In `cmd/server/main.go`, after creating the trafficScheduler:

```go
trafficScheduler.RegisterCleanupCallback(func() {
	authService.CleanupExpiredSessions()
})
```

- [ ] **Step 3: Add metrics snapshot cleanup**

In `cmd/server/main.go`, register SSE processor cleanup:

```go
trafficScheduler.RegisterCleanupCallback(func() {
	sseProcessor.CleanupOldSnapshots()
})
```

- [ ] **Step 4: Verify**

Run: `go build ./...`
Expected: PASS

---

## Phase 2: PostgreSQL Migration

This phase replaces SQLite with PostgreSQL entirely. Each task modifies the database layer and all raw SQL.

---

### Task 7: Add PostgreSQL driver dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-updated)

- [ ] **Step 1: Add pgx driver**

Run:
```bash
go get gorm.io/driver/postgres
go get github.com/jackc/pgx/v5
```

- [ ] **Step 2: Verify dependency added**

Run: `go mod tidy`
Expected: `gorm.io/driver/postgres` appears in go.mod

---

### Task 8: Rewrite db/config.go for PostgreSQL DSN

**Files:**
- Modify: `internal/db/config.go`

- [ ] **Step 1: Replace DBConfig struct**

```go
package db

import (
	log "NodePassDash/internal/log"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

// DBConfig 数据库配置结构
type DBConfig struct {
	Host         string        // PostgreSQL 主机
	Port         int           // PostgreSQL 端口
	User         string        // 用户名
	Password     string        // 密码
	Database     string        // 数据库名
	SSLMode      string        // SSL 模式
	MaxOpenConns int           // 最大打开连接数
	MaxIdleConns int           // 最大空闲连接数
	MaxLifetime  time.Duration // 连接最大生命周期
	MaxIdleTime  time.Duration // 空闲连接最大生命周期
	LogLevel     string        // 日志级别
}

// GetDBConfig 获取数据库配置
func GetDBConfig() DBConfig {
	config := DBConfig{
		Host:         "localhost",
		Port:         5432,
		User:         "nodepassdash",
		Password:     "",
		Database:     "nodepassdash",
		SSLMode:      "disable",
		MaxOpenConns: 25,
		MaxIdleConns: 10,
		MaxLifetime:  5 * time.Minute,
		MaxIdleTime:  2 * time.Minute,
		LogLevel:     "silent",
	}

	loadFromEnv(&config)
	loadFromFlags(&config)

	if err := validateConfig(&config); err != nil {
		log.Errorf("数据库配置验证失败: %v", err)
	}

	return config
}

func loadFromFlags(config *DBConfig) {
	if !flag.Parsed() {
		return
	}
	if v := flag.Lookup("db-host"); v != nil {
		config.Host = v.Value.String()
	}
	if v := flag.Lookup("db-port"); v != nil {
		if val, err := strconv.Atoi(v.Value.String()); err == nil {
			config.Port = val
		}
	}
	if v := flag.Lookup("db-user"); v != nil {
		config.User = v.Value.String()
	}
	if v := flag.Lookup("db-password"); v != nil {
		config.Password = v.Value.String()
	}
	if v := flag.Lookup("db-name"); v != nil {
		config.Database = v.Value.String()
	}
	if v := flag.Lookup("db-sslmode"); v != nil {
		config.SSLMode = v.Value.String()
	}
	if v := flag.Lookup("db-max-open"); v != nil {
		if val, err := strconv.Atoi(v.Value.String()); err == nil {
			config.MaxOpenConns = val
		}
	}
	if v := flag.Lookup("db-max-idle"); v != nil {
		if val, err := strconv.Atoi(v.Value.String()); err == nil {
			config.MaxIdleConns = val
		}
	}
	if v := flag.Lookup("db-log-level"); v != nil {
		config.LogLevel = v.Value.String()
	}
}

func loadFromEnv(config *DBConfig) {
	if v := os.Getenv("DB_HOST"); v != "" {
		config.Host = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			config.Port = val
		}
	}
	if v := os.Getenv("DB_USER"); v != "" {
		config.User = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		config.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		config.Database = v
	}
	if v := os.Getenv("DB_SSLMODE"); v != "" {
		config.SSLMode = v
	}
	if v := os.Getenv("DB_MAX_OPEN_CONNS"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			config.MaxOpenConns = val
		}
	}
	if v := os.Getenv("DB_MAX_IDLE_CONNS"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			config.MaxIdleConns = val
		}
	}
	if v := os.Getenv("DB_MAX_LIFETIME"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.MaxLifetime = d
		}
	}
	if v := os.Getenv("DB_MAX_IDLE_TIME"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.MaxIdleTime = d
		}
	}
	if v := os.Getenv("DB_LOG_LEVEL"); v != "" {
		config.LogLevel = v
	}
}

func validateConfig(config *DBConfig) error {
	if config.Host == "" {
		return fmt.Errorf("数据库主机不能为空")
	}
	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("端口号必须在 1-65535 之间")
	}
	if config.Database == "" {
		return fmt.Errorf("数据库名不能为空")
	}
	if config.MaxOpenConns <= 0 {
		return fmt.Errorf("最大连接数必须大于0")
	}
	if config.MaxIdleConns <= 0 {
		return fmt.Errorf("最大空闲连接数必须大于0")
	}
	if config.MaxIdleConns > config.MaxOpenConns {
		return fmt.Errorf("最大空闲连接数不能超过最大连接数")
	}
	return nil
}

// BuildDSN 构建 PostgreSQL 连接字符串
func (c *DBConfig) BuildDSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=Asia/Shanghai",
		c.Host, c.User, c.Password, c.Database, c.Port, c.SSLMode,
	)
}

// PrintConfig 打印配置信息
func (c *DBConfig) PrintConfig() {
	log.Infof("PostgreSQL 数据库配置:")
	log.Infof("  主机: %s:%d", c.Host, c.Port)
	log.Infof("  数据库: %s", c.Database)
	log.Infof("  用户: %s", c.User)
	log.Infof("  最大连接数: %d", c.MaxOpenConns)
	log.Infof("  最大空闲连接数: %d", c.MaxIdleConns)
	log.Infof("  连接生命周期: %v", c.MaxLifetime)
	log.Infof("  空闲超时: %v", c.MaxIdleTime)
	log.Infof("  日志级别: %s", c.LogLevel)
}
```

- [ ] **Step 2: Verify**

Run: `go build ./...`
Expected: Will fail due to db.go references — that's fixed in next task.

---

### Task 9: Rewrite db/db.go for PostgreSQL

**Files:**
- Modify: `internal/db/db.go`

- [ ] **Step 1: Replace imports and initialization**

Replace the entire file content. Key changes:
- Remove `github.com/mattn/go-sqlite3` and `gorm.io/driver/sqlite`
- Add `gorm.io/driver/postgres`
- Remove `handleDockerComposeMigration()` (SQLite-specific)
- Remove `isRetryableError()` SQLite-specific errors, replace with PG equivalents
- Remove `createPerformanceIndexes()` SQLite syntax, use PG syntax
- Update `AutoMigrate()` to use PG-compatible `SELECT COUNT(*) FROM information_schema.tables`

```go
package db

import (
	applog "NodePassDash/internal/log"
	"NodePassDash/internal/models"
	"context"
	"database/sql"
	"fmt"
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

		dsn := config.BuildDSN()

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

		gormDB, err = gorm.Open(postgres.Open(dsn), gormConfig)
		if err != nil {
			log.Fatalf("连接 PostgreSQL 数据库失败: %v", err)
		}

		sqlDB, err := gormDB.DB()
		if err != nil {
			log.Fatalf("获取 sql.DB 失败: %v", err)
		}

		sqlDB.SetMaxOpenConns(config.MaxOpenConns)
		sqlDB.SetMaxIdleConns(config.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(config.MaxLifetime)
		sqlDB.SetConnMaxIdleTime(config.MaxIdleTime)

		if err := sqlDB.Ping(); err != nil {
			log.Fatalf("数据库连接测试失败: %v", err)
		}

		if err := AutoMigrate(gormDB); err != nil {
			log.Fatalf("数据库迁移失败: %v", err)
		}

		if err := createPerformanceIndexes(gormDB); err != nil {
			log.Printf("创建性能索引失败: %v", err)
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
			return
		case <-ticker.C:
		}

		if gormDB == nil {
			continue
		}

		sqlDB, err := gormDB.DB()
		if err != nil {
			log.Printf("健康检查：获取sql.DB失败: %v", err)
			continue
		}

		if err := sqlDB.Ping(); err != nil {
			log.Printf("健康检查：数据库连接异常: %v", err)
			if strings.Contains(err.Error(), "connection refused") ||
				strings.Contains(err.Error(), "database is closed") {
				return
			}
		}

		stats := sqlDB.Stats()
		if stats.OpenConnections > int(float64(stats.MaxOpenConnections)*0.8) {
			log.Printf("警告：连接池使用率较高 %d/%d", stats.OpenConnections, stats.MaxOpenConnections)
		}
	}
}

// AutoMigrate 自动迁移数据库表结构
func AutoMigrate(db *gorm.DB) error {
	var tableCount int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'").Scan(&tableCount)

	if tableCount == 0 {
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
		&models.TrafficHourlySummary{},
		&models.DashboardTrafficSummary{},
		&models.ServiceHistory{},
		&models.Services{},
	)
}

func createPerformanceIndexes(db *gorm.DB) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_instance_record_time ON service_history (instance_id, record_time)",
	}
	for _, idx := range indexes {
		if err := db.Exec(idx).Error; err != nil {
			return fmt.Errorf("创建索引失败 [%s]: %v", idx, err)
		}
	}
	return nil
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

// ExecuteWithRetry 带重试机制的数据库执行
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
			log.Printf("数据库操作失败，%v后重试 (第%d次): %v", delay, i+1, err)
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
			log.Printf("数据库事务失败，%v后重试 (第%d次): %v", delay, i+1, err)
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
		contains(errStr, "deadlock detected") ||
		contains(errStr, "could not serialize") ||
		contains(errStr, "database is closed") ||
		contains(errStr, "too many connections")
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
		log.Printf("获取sql.DB失败，重新初始化连接: %v", err)
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
		}
	}()
}

func UpdateEndpointTunnelCountSync(endpointID int64) error {
	return ExecuteWithRetry(func(db *gorm.DB) error {
		return db.Model(&models.Endpoint{}).Where("id = ?", endpointID).
			Update("tunnel_count", db.Model(&models.Tunnel{}).Where("endpoint_id = ?", endpointID).Count(nil)).Error
	})
}
```

- [ ] **Step 2: Remove unused imports**

Remove `io`, `os`, `path/filepath` (only used by handleDockerComposeMigration and copyFile).

- [ ] **Step 3: Verify**

Run: `go build ./...`
Expected: Will fail due to SQLite raw SQL in other files — fixed in subsequent tasks.

---

### Task 10: Rewrite all SQLite raw SQL to PostgreSQL

This is the largest task. Every file with raw SQL must be updated.

**Files:**
- Modify: `internal/dashboard/traffic_service.go` (INSERT OR REPLACE → INSERT ... ON CONFLICT)
- Modify: `internal/dashboard/cleanup_service.go` (DELETE ... LIMIT subquery → PG syntax)
- Modify: `internal/api/tunnel_metrics.go` (if any SQLite-specific SQL)
- Modify: `internal/db/db.go` (sqlite_master → information_schema)
- Modify: Any other files with raw SQL

- [ ] **Step 1: Rewrite traffic_service.go**

Replace all `INSERT OR REPLACE INTO` with `INSERT INTO ... ON CONFLICT DO UPDATE SET`:

In `AggregateTrafficDataForHour()` at line 40:

```go
if err := tx.Exec(`
	INSERT INTO traffic_hourly_summary (
		hour_time, instance_id, endpoint_id,
		tcp_rx_total, tcp_tx_total, udp_rx_total, udp_tx_total,
		tcp_rx_increment, tcp_tx_increment, udp_rx_increment, udp_tx_increment,
		record_count, created_at, updated_at
	)
	SELECT 
		?, sh.instance_id, sh.endpoint_id,
		sh.delta_tcp_in, sh.delta_tcp_out, sh.delta_udp_in, sh.delta_udp_out,
		sh.delta_tcp_in, sh.delta_tcp_out, sh.delta_udp_in, sh.delta_udp_out,
		1, NOW(), NOW()
	FROM service_history sh
	INNER JOIN (
		SELECT endpoint_id, instance_id, MAX(record_time) as max_record_time
		FROM service_history
		WHERE record_time <= ?
		GROUP BY endpoint_id, instance_id
	) latest ON sh.endpoint_id = latest.endpoint_id 
		AND sh.instance_id = latest.instance_id 
		AND sh.record_time = latest.max_record_time
	WHERE sh.record_time <= ?
	ON CONFLICT (hour_time, instance_id) DO UPDATE SET
		tcp_rx_total = EXCLUDED.tcp_rx_total,
		tcp_tx_total = EXCLUDED.tcp_tx_total,
		udp_rx_total = EXCLUDED.udp_rx_total,
		udp_tx_total = EXCLUDED.udp_tx_total,
		tcp_rx_increment = EXCLUDED.tcp_rx_increment,
		tcp_tx_increment = EXCLUDED.tcp_tx_increment,
		udp_rx_increment = EXCLUDED.udp_rx_increment,
		udp_tx_increment = EXCLUDED.udp_tx_increment,
		record_count = EXCLUDED.record_count,
		updated_at = NOW()`,
	hourStart, targetTime, targetTime).Error; err != nil {
	return fmt.Errorf("插入汇总数据失败: %v", err)
}
```

**Important:** The `traffic_hourly_summary` model needs a unique constraint on `(hour_time, instance_id)`. Add to the model:

```go
type TrafficHourlySummary struct {
	ID         int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	HourTime   time.Time `json:"hourTime" gorm:"not null;uniqueIndex:idx_hour_instance"`
	InstanceID string    `json:"instanceId" gorm:"type:text;not null;uniqueIndex:idx_hour_instance"`
	// ... rest unchanged
}
```

Do the same for `initializeTrafficDataForHour()` and `aggregateDashboardTraffic()`.

For `CleanOldTrafficData()`, replace `datetime('now', '-30 days')` with `NOW() - INTERVAL '30 days'`:

```go
func (s *TrafficService) CleanOldTrafficData() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			DELETE FROM endpoint_sse_events
			WHERE event_time < NOW() - INTERVAL '30 days'
			AND event_type IN ('initial', 'update')
		`).Error; err != nil {
			return fmt.Errorf("清理原始流量数据失败: %v", err)
		}

		if err := tx.Exec(`
			DELETE FROM service_history
			WHERE record_time < NOW() - INTERVAL '7 days'
		`).Error; err != nil {
			return fmt.Errorf("清理service_history数据失败: %v", err)
		}

		if err := tx.Exec(`
			DELETE FROM traffic_hourly_summary
			WHERE hour_time < NOW() - INTERVAL '1 year'
		`).Error; err != nil {
			return fmt.Errorf("清理汇总流量数据失败: %v", err)
		}

		if err := tx.Exec(`
			DELETE FROM dashboard_traffic_summary
			WHERE hour_time < NOW() - INTERVAL '1 year'
		`).Error; err != nil {
			return fmt.Errorf("清理dashboard汇总数据失败: %v", err)
		}

		return nil
	})
}
```

- [ ] **Step 2: Rewrite cleanup_service.go**

Replace all SQLite `DELETE WHERE id IN (SELECT ... LIMIT ?)` with PostgreSQL `DELETE WHERE id IN (SELECT id ... LIMIT ?)` — this syntax actually works in PG too, but we can also use CTE:

```go
func (s *CleanupService) cleanupSSEData() CleanupResult {
	start := time.Now()
	result := CleanupResult{
		TableName: "endpoint_sse_events",
		Duration:  0,
	}

	cutoffTime := time.Now().AddDate(0, 0, -s.config.SSEDataRetentionDays)
	totalDeleted := int64(0)
	batchSize := s.config.BatchSize

	for {
		var deletedCount int64
		err := s.db.Exec(`
			DELETE FROM endpoint_sse_events
			WHERE id IN (
				SELECT id FROM endpoint_sse_events
				WHERE event_time < ?
				LIMIT ?
			)
		`, cutoffTime, batchSize).Error

		if err != nil {
			result.Error = fmt.Errorf("删除SSE数据失败: %v", err)
			break
		}

		deletedCount = s.db.RowsAffected
		totalDeleted += deletedCount

		if deletedCount < int64(batchSize) {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	result.DeletedCount = totalDeleted
	result.Duration = time.Since(start)
	return result
}
```

Apply the same pattern to `cleanupServiceHistory()`, `cleanupSummaryData()`, `cleanupOperationLogs()`.

In `optimizeTables()`, replace SQLite VACUUM with PostgreSQL VACUUM ANALYZE:

```go
func (s *CleanupService) optimizeTables() CleanupResult {
	start := time.Now()
	result := CleanupResult{
		TableName: "database_optimization",
		Duration:  0,
	}

	// PostgreSQL VACUUM ANALYZE 回收空间并更新统计信息
	// 注意：VACUUM 不能在事务中执行
	sqlDB, err := s.db.DB()
	if err != nil {
		result.Error = fmt.Errorf("获取 sql.DB 失败: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	tables := []string{
		"endpoint_sse_events",
		"service_history",
		"traffic_hourly_summary",
		"tunnel_operation_logs",
	}

	for _, table := range tables {
		if _, err := sqlDB.Exec(fmt.Sprintf("VACUUM ANALYZE %s", table)); err != nil {
			log.Printf("[数据清理] VACUUM ANALYZE %s 失败: %v", table, err)
		}
	}

	result.Duration = time.Since(start)
	return result
}
```

In `GetCleanupStats()`, replace `endpoint_sse` with `endpoint_sse_events`.

- [ ] **Step 3: Check for any other SQLite-specific SQL**

Search for: `datetime('now'`, `INSERT OR REPLACE`, `sqlite_master`, `PRAGMA`

Run: `grep -rn "datetime\|INSERT OR REPLACE\|sqlite_master\|PRAGMA" internal/`

Fix any remaining occurrences.

- [ ] **Step 4: Verify**

Run: `go build ./...`
Expected: PASS (or identify remaining issues)

---

### Task 11: Update models for PostgreSQL compatibility

**Files:**
- Modify: `internal/models/models.go` (type adjustments)
- Modify: `internal/models/traffic.go` (if needed)
- Modify: `internal/models/time_field.go` (NullTime for PG)

- [ ] **Step 1: Review NullTime type**

`NullTime` at `internal/models/time_field.go` uses `datetime` format for SQLite. For PostgreSQL, it should work with `time.Time` directly. However, since GORM handles this, the NullTime type should still work. Verify by checking if `Scan`/`Value` methods handle PG correctly.

If NullTime has issues, replace with `*time.Time`:

```go
// In models.go, change:
LastEventTime NullTime `json:"lastEventTime,omitempty" gorm:"column:last_event_time;type:timestamp"`
// To:
LastEventTime *time.Time `json:"lastEventTime,omitempty" gorm:"column:last_event_time"`
```

- [ ] **Step 2: Add unique constraints needed for UPSERT**

The `traffic_hourly_summary` table needs a unique constraint on `(hour_time, instance_id)` for `ON CONFLICT` to work:

```go
type TrafficHourlySummary struct {
	ID         int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	HourTime   time.Time `json:"hourTime" gorm:"not null;uniqueIndex:idx_hour_instance"`
	InstanceID string    `json:"instanceId" gorm:"type:text;not null;uniqueIndex:idx_hour_instance"`
	EndpointID int64     `json:"endpointId" gorm:"not null"`
	// ... rest unchanged
}
```

Similarly for `dashboard_traffic_summary` if it uses UPSERT.

- [ ] **Step 3: Verify**

Run: `go build ./...`
Expected: PASS

---

### Task 12: Update Docker and deployment files

**Files:**
- Modify: `Dockerfile` (remove CGO dependency for SQLite)
- Modify: `docker-compose.yml` (add PostgreSQL service)
- Modify: `docker-compose-dev.yml` (add PostgreSQL service)
- Modify: `scripts/install.sh` (add PostgreSQL installation)

- [ ] **Step 1: Update docker-compose.yml**

Add PostgreSQL service:

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: nodepassdash
      POSTGRES_PASSWORD: ${DB_PASSWORD:-nodepassdash}
      POSTGRES_DB: nodepassdash
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U nodepassdash"]
      interval: 10s
      timeout: 5s
      retries: 5

  nodepassdash:
    image: ghcr.io/nodepassproject/nodepassdash:latest
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      DB_HOST: postgres
      DB_PORT: 5432
      DB_USER: nodepassdash
      DB_PASSWORD: ${DB_PASSWORD:-nodepassdash}
      DB_NAME: nodepassdash
      DB_SSLMODE: disable
    volumes:
      - ./logs:/app/logs
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
```

- [ ] **Step 2: Update Dockerfile**

Remove CGO requirement (no longer needed without go-sqlite3):

```dockerfile
# Build stage
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache git
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /build/server ./cmd/server

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/server .
COPY --from=builder /build/cmd/server/dist ./dist
EXPOSE 3000
CMD ["./server"]
```

- [ ] **Step 3: Update docker-compose-dev.yml**

Same PostgreSQL service addition as docker-compose.yml but with local build.

- [ ] **Step 4: Update scripts/install.sh**

Add PostgreSQL installation and configuration. Add `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` to the systemd service environment.

- [ ] **Step 5: Remove go-sqlite3 dependency**

Run:
```bash
go mod tidy
```

Verify `github.com/mattn/go-sqlite3` is removed from go.mod.

- [ ] **Step 6: Verify**

Run: `go build ./...`
Expected: PASS

---

### Task 13: Add SQLite → PostgreSQL data migration tool

**Files:**
- Create: `cmd/migrate/main.go`

- [ ] **Step 1: Create migration tool**

Create a standalone tool that reads from SQLite and writes to PostgreSQL:

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"NodePassDash/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	sqlitePath := flag.String("sqlite", "db/database.db", "SQLite 数据库文件路径")
	pgDSN := flag.String("pg", "", "PostgreSQL DSN (host=... user=... password=... dbname=... port=... sslmode=...)")
	flag.Parse()

	if *pgDSN == "" {
		log.Fatal("请提供 PostgreSQL DSN: -pg \"host=localhost user=nodepassdash password=xxx dbname=nodepassdash port=5432 sslmode=disable\"")
	}

	// 连接 SQLite
	sqliteDB, err := gorm.Open(sqlite.Open(*sqlitePath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("连接 SQLite 失败: %v", err)
	}

	// 连接 PostgreSQL
	pgDB, err := gorm.Open(postgres.Open(*pgDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("连接 PostgreSQL 失败: %v", err)
	}

	// 自动创建表结构
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
		log.Fatalf("PostgreSQL 表结构创建失败: %v", err)
	}

	// 按顺序迁移各表
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
		migrateTable(sqliteDB, pgDB, t.name, t.model)
	}

	log.Println("数据迁移完成!")
}

func migrateTable(src, dst *gorm.DB, tableName string, model interface{}) {
	start := time.Now()
	log.Printf("迁移表: %s ...", tableName)

	if err := src.Table(tableName).Find(model).Error; err != nil {
		log.Printf("  读取 %s 失败: %v", tableName, err)
		return
	}

	// 使用批量插入
	if err := dst.Table(tableName).Create(model).Error; err != nil {
		log.Printf("  写入 %s 失败: %v", tableName, err)
		return
	}

	log.Printf("  %s 迁移完成, 耗时: %v", tableName, time.Since(start))
}
```

- [ ] **Step 2: Verify**

Run: `go build ./cmd/migrate`
Expected: PASS

---

### Task 14: Apply SQLite solutions to PostgreSQL

The fixes from Phase 1 need to work with PostgreSQL too. Most are already compatible, but verify:

- [ ] **Step 1: service_history cleanup — already PG-compatible**

The cleanup code uses `DELETE WHERE id IN (SELECT ... LIMIT ?)` which works in PostgreSQL. The `NOW() - INTERVAL '7 days'` syntax is PostgreSQL-native. No changes needed beyond what Task 10 already did.

- [ ] **Step 2: Composite index — already PG-compatible**

`CREATE INDEX IF NOT EXISTS idx_instance_record_time ON service_history (instance_id, record_time)` works in both SQLite and PostgreSQL. Already handled in Task 9.

- [ ] **Step 3: In-memory map TTL — no DB dependency**

The HistoryWorker TTL cleanup is pure Go code, no database interaction. Works unchanged.

- [ ] **Step 4: Connection pool tuning for PostgreSQL**

PostgreSQL supports higher concurrency than SQLite. The defaults in Task 8 (25 open, 10 idle) are appropriate. No additional changes needed.

- [ ] **Step 5: Remove SQLite-specific auth cache cleanup scheduling**

The auth cache cleanup from Task 6 works unchanged — it's pure Go sync.Map operations.

- [ ] **Step 6: Final verification**

Run: `go build ./...`
Run: `go vet ./...`
Expected: PASS

---

## Task Dependency Graph

```
Task 1 (service_history cleanup)  ─┐
Task 2 (table name drift)         ─┤
Task 3 (composite index)          ─┤── Phase 1 (independent, do in parallel)
Task 4 (map TTL)                  ─┤
Task 5 (temp_store config)        ─┤
Task 6 (cache cleanup scheduling) ─┘
                                      │
Task 7 (add PG driver)            ─┐  │
Task 8 (rewrite config.go)        ─┤  │
Task 9 (rewrite db.go)            ─┤── Phase 2 (sequential: 7→8→9→10→11→12→13)
Task 10 (rewrite raw SQL)         ─┤
Task 11 (update models)           ─┤
Task 12 (Docker/deploy)           ─┤
Task 13 (migration tool)          ─┘
                                      │
Task 14 (verify PG solutions)     ──── Final verification
```

## Rollback Strategy

If the PostgreSQL migration causes issues:
1. Keep the SQLite code on a separate git branch
2. The migration tool (Task 13) is one-way; for rollback, dump PG and import into SQLite
3. Docker volumes preserve the SQLite database file at `./db/database.db`
