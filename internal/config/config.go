package config

import (
	"os"      // 用于读取环境变量
	"strconv" // 用于将字符串转换为数字等
	"time"    // 用于时间相关的配置，如超时

	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"github.com/joho/godotenv" // 用于加载 .env 文件
	"go.uber.org/zap"
)

// Config 结构体定义了应用的所有配置项
type Config struct {
	ServerAddr    string // MCP 服务器监听地址 (例如: ":8181")
	LogLevel      string // 日志级别 (例如: "debug", "info", "warn", "error")
	ExtensionsDir string // 存放扩展知识 YAML 文件的目录路径
	// --- 数据库相关配置 ---
	DBConnMaxLifetime time.Duration // 连接池中连接的最大生命周期
	DBConnMaxIdleTime time.Duration // 连接池中连接的最大空闲时间
	DBMaxOpenConns    int           // 连接池最大打开连接数
	DBMinOpenConns    int           // 连接池最小空闲连接数
	// SchemaLoadDBURL string        // (可选) 如果需要一个固定的连接串在启动时加载Schema
	// 这个可以考虑去掉，让 SchemaManager 在需要时向 DatabaseService 注册一个临时的
}

// LoadConfig 加载配置信息
// 它首先尝试加载项目根目录下的 .env 文件（如果存在），
// 然后从环境变量中读取配置项。如果环境变量未设置，则使用默认值。
func LoadConfig() *Config {
	// 尝试加载 .env 文件，忽略错误（可能文件不存在）
	err := godotenv.Load()
	if err != nil {
		utils.DefaultLogger.Error("未找到.env 配置文件错误或不存在", zap.Error(err))
	}

	cfg := &Config{
		// 设置默认值
		ServerAddr:        getEnv("MCP_SERVER_ADDR", ":8181"),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		ExtensionsDir:     getEnv("EXTENSIONS_DIR", "./extensions_knowledge"), // 默认在项目根目录下的 extensions_knowledge
		DBConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 1*time.Hour),
		DBConnMaxIdleTime: getEnvDuration("DB_CONN_MAX_IDLE_TIME", 30*time.Minute),
		DBMaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 10),
		DBMinOpenConns:    getEnvInt("DB_MIN_OPEN_CONNS", 2),
		// SchemaLoadDBURL: getEnv("SCHEMA_LOAD_DB_URL", ""), // 如果需要固定连接串加载
	}

	// 可以在这里添加对配置项的验证逻辑
	if cfg.DBMinOpenConns > cfg.DBMaxOpenConns {
		utils.DefaultLogger.Info("警告: DB_MIN_OPEN_CONNS  大于 DB_MAX_OPEN_CONNS, 将使用 DB_MAX_OPEN_CONNS 作为最小值。\n")
		cfg.DBMinOpenConns = cfg.DBMaxOpenConns
	}

	utils.DefaultLogger.Info("配置加载完成",
		zap.String("ServerAddr", cfg.ServerAddr),
		zap.String("LogLevel", cfg.LogLevel),
		zap.String("ExtensionsDir", cfg.ExtensionsDir),
	)
	return cfg
}

// --- 辅助函数 ---

// getEnv 读取环境变量，如果未设置则返回默认值
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvInt 读取环境变量并解析为整数，如果未设置或解析失败则返回默认值
func getEnvInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		utils.DefaultLogger.Warn("警告: 无法将环境变量解析为整数, 将使用默认值",
			zap.String("key", key),
			zap.String("value", valueStr),
			zap.Error(err),
			zap.Int("defaultValue", defaultValue),
		)
		return defaultValue
	}
	return value
}

// getEnvDuration 读取环境变量并解析为时间段，如果未设置或解析失败则返回默认值
// 期望格式如 "1h", "30m", "10s"
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(valueStr)
	if err != nil {
		utils.DefaultLogger.Warn("警告: 无法将环境变量解析为整数, 将使用默认值",
			zap.String("key", key),
			zap.String("value", valueStr),
			zap.Error(err),
			zap.Duration("defaultValue", defaultValue),
		)
		return defaultValue
	}
	return value
}
