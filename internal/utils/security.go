package utils

import (
	"fmt"
	"strings"
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
