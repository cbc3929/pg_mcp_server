package utils

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// DefaultLogger 保存配置好的 zap 日志记录器实例。
// 它由 SetupLogger 函数配置。
var DefaultLogger *zap.Logger

// SetupLogger 初始化 zap 日志记录器。
// 它根据 debugMode 标志配置日志级别、编码器和输出。
// 在调试模式下，级别为 Debug，输出更易读，并包含调用者信息。
// 在发布模式下，级别为 Info，输出为 JSON 格式，性能更高。
func SetupLogger(debugMode bool) {
	var zapConfig zap.Config
	var level zapcore.Level

	if debugMode {
		// 使用 zap 提供的开发环境预设配置，易于阅读
		zapConfig = zap.NewDevelopmentConfig()
		level = zapcore.DebugLevel
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // 彩色级别显示
		zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder        // 标准时间格式
		zapConfig.DisableStacktrace = true                                     // 开发模式下通常不需要完整堆栈跟踪，除非是Error级别以上
		zapConfig.EncoderConfig.CallerKey = "caller"                           // 显示调用者信息
		zapConfig.EncoderConfig.NameKey = "logger"
		zapConfig.EncoderConfig.MessageKey = "msg"
	} else {
		// 使用 zap 提供的生产环境预设配置，性能优先，JSON 格式
		zapConfig = zap.NewProductionConfig()
		level = zapcore.InfoLevel
		zapConfig.EncoderConfig.TimeKey = "ts"                              // 时间戳字段名
		zapConfig.EncoderConfig.EncodeTime = zapcore.EpochMillisTimeEncoder // 使用毫秒时间戳
		zapConfig.EncoderConfig.CallerKey = ""                              // 生产环境通常不记录调用者，以提高性能
	}

	// 设置日志级别
	zapConfig.Level = zap.NewAtomicLevelAt(level)

	// 设置输出到标准输出
	zapConfig.OutputPaths = []string{"stdout"}
	zapConfig.ErrorOutputPaths = []string{"stderr"}

	// 构建 logger
	var err error
	DefaultLogger, err = zapConfig.Build()
	if err != nil {
		// 如果构建 logger 失败，这是一个严重问题，直接 panic
		panic(fmt.Sprintf("无法初始化 zap 日志记录器: %v", err))
	}

	// 可选：替换 zap 的全局 logger，这样可以在任何地方通过 zap.L() 或 zap.S() 访问
	// zap.ReplaceGlobals(DefaultLogger)
	// zap.S().Info("全局 SugaredLogger 已替换") // 示例：使用全局 SugaredLogger

	DefaultLogger.Info("Zap 日志记录器已初始化",
		zap.Bool("debugMode", debugMode),
		zap.String("logLevel", level.String()),
	)
}

// GetLogger 返回配置好的 zap 日志记录器实例。
// 在使用此函数之前，应先调用 SetupLogger。
func GetLogger() *zap.Logger {
	if DefaultLogger == nil {
		// 如果 DefaultLogger 尚未初始化，这是一个编程错误。
		// 最安全的做法是返回一个 Nop logger，它什么也不做。
		// 或者 panic，强制要求在使用前必须初始化。
		// 这里我们选择返回 Nop logger 并打印一个标准库错误。
		println("警告: GetLogger 在 SetupLogger 被调用之前被访问。返回 Nop logger。") // 使用标准 println 避免依赖未初始化的 logger
		return zap.NewNop()
	}
	return DefaultLogger
}

// SyncLogger 刷新所有缓冲的日志条目。
// 建议在应用程序退出前调用此函数（例如在 main 函数的 defer 中）。
func SyncLogger() {
	if DefaultLogger != nil {
		_ = DefaultLogger.Sync() // 忽略 sync 的错误
	}
}

// --- 可选：提供 Sugared Logger 的便捷访问 ---

// GetSugaredLogger 返回配置好的 zap SugaredLogger 实例。
// SugaredLogger 提供了更便捷的 API（如 Printf, Infow），但性能略低于 Logger。
func GetSugaredLogger() *zap.SugaredLogger {
	if DefaultLogger == nil {
		println("警告: GetSugaredLogger 在 SetupLogger 被调用之前被访问。返回 Nop logger 的 Sugared 版本。")
		return zap.NewNop().Sugar()
	}
	return DefaultLogger.Sugar()
}
