package db

import (
	log "NodePassDash/internal/log"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

// DBConfig PostgreSQL数据库配置结构
type DBConfig struct {
	Host         string        // 数据库主机
	Port         int           // 数据库端口
	User         string        // 数据库用户
	Password     string        // 数据库密码
	Database     string        // 数据库名称
	SSLMode      string        // SSL模式
	MaxOpenConns int           // 最大打开连接数
	MaxIdleConns int           // 最大空闲连接数
	MaxLifetime  time.Duration // 连接最大生命周期
	MaxIdleTime  time.Duration // 空闲连接最大生命周期
	LogLevel     string        // 日志级别
}

// GetDBConfig 获取数据库配置，支持多种来源
func GetDBConfig() DBConfig {
	config := DBConfig{
		// 默认值 - PostgreSQL配置
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

	// 1. 从命令行参数读取
	loadFromFlags(&config)

	// 2. 从环境变量读取（优先级更高）
	loadFromEnv(&config)

	// 3. 从配置文件读取（如果存在）
	loadFromFile(&config)

	// 验证配置
	if err := validateConfig(&config); err != nil {
		log.Errorf("数据库配置验证失败: %v", err)
	}

	log.Infof("数据库配置: %s@%s:%d/%s", config.User, config.Host, config.Port, config.Database)
	return config
}

// loadFromFlags 从命令行参数加载配置
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

// loadFromEnv 从环境变量加载配置
func loadFromEnv(config *DBConfig) {
	if value := os.Getenv("DB_HOST"); value != "" {
		config.Host = value
	}
	if value := os.Getenv("DB_PORT"); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			config.Port = intVal
		}
	}
	if value := os.Getenv("DB_USER"); value != "" {
		config.User = value
	}
	if value := os.Getenv("DB_PASSWORD"); value != "" {
		config.Password = value
	}
	if value := os.Getenv("DB_NAME"); value != "" {
		config.Database = value
	}
	if value := os.Getenv("DB_SSLMODE"); value != "" {
		config.SSLMode = value
	}
	if value := os.Getenv("DB_MAX_OPEN_CONNS"); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			config.MaxOpenConns = intVal
		}
	}
	if value := os.Getenv("DB_MAX_IDLE_CONNS"); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			config.MaxIdleConns = intVal
		}
	}
	if value := os.Getenv("DB_MAX_LIFETIME"); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			config.MaxLifetime = duration
		}
	}
	if value := os.Getenv("DB_MAX_IDLE_TIME"); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			config.MaxIdleTime = duration
		}
	}
	if value := os.Getenv("DB_LOG_LEVEL"); value != "" {
		config.LogLevel = value
	}
}

// loadFromFile 从配置文件加载配置（简化实现）
func loadFromFile(config *DBConfig) {
	if _, err := os.Stat(".env"); err == nil {
		log.Info("检测到 .env 文件，建议使用环境变量替代")
	}
}

// validateConfig 验证配置的有效性
func validateConfig(config *DBConfig) error {
	if config.Host == "" {
		return fmt.Errorf("数据库主机不能为空")
	}
	if config.Port < 1 || config.Port > 65535 {
		return fmt.Errorf("数据库端口必须在1-65535之间")
	}
	if config.Database == "" {
		return fmt.Errorf("数据库名称不能为空")
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

// BuildDSN 构建PostgreSQL连接字符串
func (c *DBConfig) BuildDSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=Asia/Shanghai",
		c.Host, c.User, c.Password, c.Database, c.Port, c.SSLMode,
	)
}

// PrintConfig 打印配置信息
func (c *DBConfig) PrintConfig() {
	log.Infof("PostgreSQL数据库配置:")
	log.Infof("  主机: %s", c.Host)
	log.Infof("  端口: %d", c.Port)
	log.Infof("  用户: %s", c.User)
	log.Infof("  数据库: %s", c.Database)
	log.Infof("  SSL模式: %s", c.SSLMode)
	log.Infof("  最大连接数: %d", c.MaxOpenConns)
	log.Infof("  最大空闲连接数: %d", c.MaxIdleConns)
	log.Infof("  连接生命周期: %v", c.MaxLifetime)
	log.Infof("  空闲超时: %v", c.MaxIdleTime)
	log.Infof("  日志级别: %s", c.LogLevel)
}
