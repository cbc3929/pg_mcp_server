package databases

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool" // 导入 pgx 连接池
)

// Service 定义了数据库服务的接口契约
// 这允许我们将具体的实现（如 pgx）与使用它的代码（Handlers）解耦。
type Service interface {
	// RegisterConnection 注册一个数据库连接字符串，为其生成并返回一个唯一的 connID。
	// 如果该连接字符串已注册，则返回现有的 connID。
	// ctx: 请求上下文，用于传递超时或取消信号。
	// connString: PostgreSQL 连接字符串 (例如: "postgres://user:pass@host:port/db")。
	// 返回值: connID (字符串) 和 error。
	RegisterConnection(ctx context.Context, connString string) (string, error)

	// DisconnectConnection 关闭与指定 connID 关联的连接池并移除相关映射。
	// ctx: 请求上下文。
	// connID: 要断开的连接 ID。
	// 返回值: error。
	DisconnectConnection(ctx context.Context, connID string) error

	// GetPool 获取与指定 connID 关联的 pgx 连接池。
	// 如果 connID 不存在或对应的连接池尚未初始化，此方法会尝试创建和初始化连接池。
	// ctx: 请求上下文。
	// connID: 连接 ID。
	// 返回值: *pgxpool.Pool 和 error。
	GetPool(ctx context.Context, connID string) (*pgxpool.Pool, error)

	// ExecuteQuery 执行一个查询并返回结果行。
	// ctx: 请求上下文。
	// connID: 连接 ID。
	// readOnly: 指示事务是否应以只读模式执行。对于查询 public schema 必须为 true。
	// sql: 要执行的 SQL 语句，应使用 $1, $2... 作为参数占位符。
	// args: SQL 语句对应的参数。
	// 返回值: 查询结果 (每行是一个 map[string]any) 和 error。
	ExecuteQuery(ctx context.Context, connID string, readOnly bool, sql string, args ...any) ([]map[string]any, error)

	// ExecuteNonQuery 执行一个不返回结果行的 SQL 命令（如 INSERT, UPDATE, DELETE）。
	// ctx: 请求上下文。
	// connID: 连接 ID。
	// readOnly: 指示事务是否应以只读模式执行。通常对于非查询操作为 false，但必须*极其谨慎*地用于 temp schema。
	// sql: 要执行的 SQL 命令，应使用 $1, $2... 作为参数占位符。
	// args: SQL 命令对应的参数。
	// 返回值: error。
	ExecuteNonQuery(ctx context.Context, connID string, readOnly bool, sql string, args ...any) error

	// CloseAll 关闭所有由该服务管理的连接池。通常在服务器关闭时调用。
	// ctx: 请求上下文。
	// 返回值: error。
	CloseAll(ctx context.Context) error
}
