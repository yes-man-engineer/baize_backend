package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv  string
	AppPort string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// CORSOrigins 允许跨域的前端来源。留空 = 放开全部（只该在本地开发用）。
	CORSOrigins []string

	LLMBaseURL        string
	LLMAPIKey         string
	LLMModel          string
	LLMTimeoutSeconds int

	// LLMTemperature 留空表示不给模型传这个参数。
	// Kimi 的 k2/k3 只接受 1，传别的值会被拒；DeepSeek 可以填 0.4。
	LLMTemperature *float32
}

// Load 读取 .env（不存在时忽略）与环境变量。
func Load() *Config {
	_ = godotenv.Load()

	return &Config{
		AppEnv:  env("APP_ENV", "dev"),
		AppPort: env("APP_PORT", "8080"),

		DBHost:     env("DB_HOST", "127.0.0.1"),
		DBPort:     env("DB_PORT", "3306"),
		DBUser:     env("DB_USER", "root"),
		DBPassword: env("DB_PASSWORD", ""),
		DBName:     env("DB_NAME", "startup_copilot"),

		CORSOrigins: envList("CORS_ORIGINS"),

		LLMBaseURL:        env("LLM_BASE_URL", "https://api.deepseek.com/v1"),
		LLMAPIKey:         env("LLM_API_KEY", ""),
		LLMModel:          env("LLM_MODEL", "deepseek-chat"),
		LLMTimeoutSeconds: envInt("LLM_TIMEOUT_SECONDS", 120),
		LLMTemperature:    envFloatPtr("LLM_TEMPERATURE"),
	}
}

func (c *Config) IsDev() bool { return c.AppEnv == "dev" }

// DSN 返回 MySQL 连接串。
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName,
	)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envList 读逗号分隔的列表，如 CORS_ORIGINS=https://a.com,https://b.com
func envList(key string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, it := range strings.Split(v, ",") {
		if it = strings.TrimSpace(it); it != "" {
			out = append(out, it)
		}
	}
	return out
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// envFloatPtr 读可选的浮点配置，留空或填了非法值都返回 nil。
func envFloatPtr(key string) *float32 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		return nil
	}
	value := float32(parsed)
	return &value
}
