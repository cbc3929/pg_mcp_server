package utils

import (
	"fmt"
	"github.com/google/uuid"
	"net/url"
	"strings"
)

// postgresqlNamespace 是一个用于 PostgreSQL 连接的自定义 UUID 命名空间。
var postgresqlNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("mcp.cc.com.postgresql"))

// 在生成 UUID 之前，它会对输入字符串进行规范化处理。
func ConnectionStringToUUID(connectionString string) (string, error) {
	// 确保有协议前缀以便解析
	if !strings.HasPrefix(connectionString, "postgresql://") {
		connectionString = "postgresql://" + connectionString
	}

	parsed, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("解析连接字符串失败: %w", err)
	}

	// 提取关键部分：用户（如果存在）、主机、端口、数据库路径
	user := ""
	if parsed.User != nil {
		user = parsed.User.Username() // 生成 ID 时排除密码
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "5432" // PostgreSQL 默认端口
	}
	// 路径通常包含前导 "/"，移除它以保持一致性。如果为空则使用 "postgres"。
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		dbName = "postgres" // 如果未指定，则为默认数据库名
	}

	// 创建用于 UUID 生成的规范化字符串表示
	// 示例: "user@host:port/dbname"
	canonicalString := fmt.Sprintf("%s@%s:%s/%s", user, host, port, dbName)

	// 使用自定义命名空间生成版本 5 UUID (基于 SHA-1)
	resultUUID := uuid.NewSHA1(postgresqlNamespace, []byte(canonicalString))
	return resultUUID.String(), nil
}
