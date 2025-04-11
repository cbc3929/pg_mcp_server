package utils

import (
	"fmt"
	"math"
	"strings"

	"go.uber.org/zap"
)

// 它将标识符用双引号包裹起来，并对标识符内已有的双引号进行转义（双写）。
func QuoteIdentifier(identifier string) string {
	// 将已有的双引号替换为两个双引号
	escapedIdentifier := strings.ReplaceAll(identifier, `"`, `""`)
	// 将结果用双引号包裹
	return fmt.Sprintf(`"%s"`, escapedIdentifier)
}

// QuoteLiteral 安全地引用用于 SQL 查询中的字符串字面量。
func QuoteLiteral(literal string) string {
	// 将已有的单引号替换为两个单引号
	escapedLiteral := strings.ReplaceAll(literal, `'`, `''`)
	// 将反斜杠替换为两个反斜杠
	escapedLiteral = strings.ReplaceAll(escapedLiteral, `\`, `\\`)

	// 使用 PostgreSQL 的转义字符串语法 (E'...')，它可以正确处理反斜杠。
	return fmt.Sprintf(`E'%s'`, escapedLiteral)
}

// TODO 更加安全的过滤机制 来防止注入的问题 SanitizeSQLString 是一个占位符，
func SanitizeSQLString(input string) string {
	sanitized := input
	// 移除常见的注释语法
	sanitized = strings.ReplaceAll(sanitized, "--", "")
	sanitized = strings.ReplaceAll(sanitized, "/*", "")
	sanitized = strings.ReplaceAll(sanitized, "*/", "")
	// 如果试图阻止多个语句，移除分号（但这可能破坏有效的输入）
	// sanitized = strings.ReplaceAll(sanitized, ";", "")
	return sanitized
}
func DbInt64(value any) int64 {
	if value == nil {
		return 0 // nil 值视为 0
	}

	switch v := value.(type) {
	case int64:
		return v // 最理想情况
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int16:
		return int64(v)
	case int8:
		return int64(v)
	case uint:
		// 注意: uint 可能超出 int64 范围，这里做个检查
		if uint64(v) > math.MaxInt64 {
			DefaultLogger.Warn("uint 值超出 int64 最大值，将截断", zap.Uint("original", v), zap.Int64("maxInt64", math.MaxInt64))
			return math.MaxInt64
		}
		return int64(v)
	case uint64:
		if v > math.MaxInt64 {
			DefaultLogger.Warn("uint64 值超出 int64 最大值，将截断", zap.Uint64("original", v), zap.Int64("maxInt64", math.MaxInt64))
			return math.MaxInt64
		}
		return int64(v)
	case uint32:
		return int64(v) // 不会超出 int64
	case uint16:
		return int64(v)
	case uint8:
		return int64(v)
	case float64:
		// 浮点数转整数会丢失小数部分，并可能溢出
		if v < float64(math.MinInt64) || v > float64(math.MaxInt64) {
			DefaultLogger.Warn("float64 值超出 int64 范围，将截断", zap.Float64("original", v))
			if v < 0 {
				return math.MinInt64
			}
			return math.MaxInt64
		}
		if v != math.Trunc(v) { // 检查是否有小数部分
			DefaultLogger.Debug("float64 值转换为 int64 时将丢失小数部分", zap.Float64("original", v), zap.Int64("truncated", int64(v)))
		}
		return int64(v)
	case float32:
		f64 := float64(v) // 转换为 float64 处理
		if f64 < float64(math.MinInt64) || f64 > float64(math.MaxInt64) {
			DefaultLogger.Warn("float32 值超出 int64 范围，将截断", zap.Float32("original", v))
			if f64 < 0 {
				return math.MinInt64
			}
			return math.MaxInt64
		}
		if f64 != math.Trunc(f64) {
			DefaultLogger.Debug("float32 值转换为 int64 时将丢失小数部分", zap.Float32("original", v), zap.Int64("truncated", int64(f64)))
		}
		return int64(f64)
	case string:
		// 尝试从字符串解析，如果需要的话。通常数据库不会直接返回数字字符串，除非查询特殊处理过。
		// 如果需要支持字符串，需要引入 strconv.ParseInt
		// i, err := strconv.ParseInt(v, 10, 64)
		// if err == nil {
		//     return i
		// }
		DefaultLogger.Warn("尝试将字符串转换为 int64，但此函数默认不支持", zap.String("value", v))
		return 0
	default:
		// 其他未知类型
		DefaultLogger.Warn("预期数据库返回数字类型，但类型不匹配", zap.Any("value", value), zap.String("type", fmt.Sprintf("%T", value)))
		return 0
	}
}
