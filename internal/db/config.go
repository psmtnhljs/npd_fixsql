package db

import (
	log "NodePassDash/internal/log"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type DBConfig struct {
	DatabaseURL  string
	Host         string
	Port         int
	User         string
	Password     string
	Database     string
	SSLMode      string
	TimeZone     string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
	MaxIdleTime  time.Duration
	LogLevel     string
}

func GetDBConfig() DBConfig {
	config := DBConfig{
		Host:         "127.0.0.1",
		Port:         5432,
		User:         "postgres",
		Password:     "",
		Database:     "nodepassdash",
		SSLMode:      "disable",
		TimeZone:     "Asia/Shanghai",
		MaxOpenConns: 25,
		MaxIdleConns: 10,
		MaxLifetime:  30 * time.Minute,
		MaxIdleTime:  10 * time.Minute,
		LogLevel:     "silent",
	}

	loadFromEnv(&config)

	if err := validateConfig(&config); err != nil {
		log.Errorf("数据库配置校验失败: %v", err)
	}

	log.Infof("数据库配置: %s", config.SafeDSN())
	return config
}

func loadFromEnv(config *DBConfig) {
	if value := os.Getenv("DATABASE_URL"); value != "" {
		config.DatabaseURL = value
	}
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
	if value := os.Getenv("DB_TIMEZONE"); value != "" {
		config.TimeZone = value
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

func validateConfig(config *DBConfig) error {
	if config.DatabaseURL == "" {
		if config.Host == "" {
			return fmt.Errorf("DB_HOST 不能为空")
		}
		if config.Port <= 0 {
			return fmt.Errorf("DB_PORT 必须大于 0")
		}
		if config.User == "" {
			return fmt.Errorf("DB_USER 不能为空")
		}
		if config.Database == "" {
			return fmt.Errorf("DB_NAME 不能为空")
		}
	} else if _, err := url.Parse(config.DatabaseURL); err != nil {
		return fmt.Errorf("DATABASE_URL 无效: %w", err)
	}

	if config.MaxOpenConns <= 0 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS 必须大于 0")
	}
	if config.MaxIdleConns < 0 {
		return fmt.Errorf("DB_MAX_IDLE_CONNS 不能小于 0")
	}
	if config.MaxIdleConns > config.MaxOpenConns {
		return fmt.Errorf("DB_MAX_IDLE_CONNS 不能大于 DB_MAX_OPEN_CONNS")
	}

	return nil
}

func (c *DBConfig) BuildDSN() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}

	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		c.Host,
		c.Port,
		c.User,
		c.Password,
		c.Database,
		c.SSLMode,
		c.TimeZone,
	)
}

func (c *DBConfig) SafeDSN() string {
	if c.DatabaseURL != "" {
		parsed, err := url.Parse(c.DatabaseURL)
		if err == nil {
			if parsed.User != nil {
				username := parsed.User.Username()
				if _, ok := parsed.User.Password(); ok {
					parsed.User = url.UserPassword(username, "******")
				}
			}
			return parsed.String()
		}
		return "DATABASE_URL=***"
	}

	return fmt.Sprintf(
		"host=%s port=%d user=%s dbname=%s sslmode=%s TimeZone=%s",
		c.Host,
		c.Port,
		c.User,
		c.Database,
		c.SSLMode,
		c.TimeZone,
	)
}

func (c *DBConfig) PrintConfig() {
	log.Infof("PostgreSQL 数据库配置")
	log.Infof("  连接: %s", c.SafeDSN())
	log.Infof("  最大连接数: %d", c.MaxOpenConns)
	log.Infof("  最大空闲连接数: %d", c.MaxIdleConns)
	log.Infof("  连接生命周期: %v", c.MaxLifetime)
	log.Infof("  空闲超时: %v", c.MaxIdleTime)
	log.Infof("  日志级别: %s", c.LogLevel)
}
