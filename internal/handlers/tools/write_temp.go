package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

// WriteTempHandler 处理向 temp schema 写入数据的工具调用。
// !! 极度重要: 这个处理器的实现必须非常小心，以防止安全风险 !!
type WriteTempHandler struct {
	dbService databases.Service
}

// NewWriteTempHandler 创建一个新的 WriteTempHandler。
func NewWriteTempHandler(dbService databases.Service) *WriteTempHandler {
	return &WriteTempHandler{dbService: dbService}
}

// HandleSaveAnalysisResult (示例) 处理将分析结果保存到 temp 表的请求。
func (h *WriteTempHandler) HandleSaveAnalysisResult(ctx context.Context, req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	utils.DefaultLogger.Warn("收到 'save_analysis_result' (写入操作) 工具调用请求，需谨慎处理！", zap.Any("args", req.Arguments))

	// 1. 提取和验证参数
	connID, ok := req.Arguments["conn_id"].(string)
	if !ok || connID == "" {
		return nil, fmt.Errorf("缺少或无效的 'conn_id'")
	}

	// 目标表名 - 必须严格限制在 temp schema 下，并且进行清理
	targetTableNameSuffix, ok := req.Arguments["target_table_name_suffix"].(string)
	if !ok || targetTableNameSuffix == "" {
		return nil, fmt.Errorf("缺少 'target_table_name_suffix'")
	}
	// 清理表名后缀，只允许字母、数字、下划线
	safeSuffix := utils.SanitizeSQLString(targetTableNameSuffix)
	if safeSuffix == "" {
		return nil, fmt.Errorf("无效的 'target_table_name_suffix' (清理后为空)")
	}
	// 构造完整的、带 schema 前缀的表名
	// 使用会话ID或任务ID确保唯一性，防止冲突（这里用UUID模拟）
	uniqueTableName := fmt.Sprintf("temp.analysis_%s_%s", safeSuffix, utils.GenerateUUID()[:8])

	// 结果数据 - 假设是以 JSON 数组形式传入
	resultDataVal, ok := req.Arguments["result_data"] // 类型可能是 string 或 []any
	if !ok {
		return nil, fmt.Errorf("缺少 'result_data'")
	}

	var results []map[string]any
	switch data := resultDataVal.(type) {
	case string:
		if err := json.Unmarshal([]byte(data), &results); err != nil {
			return nil, fmt.Errorf("解析 'result_data' JSON 字符串失败: %w", err)
		}
	case []any:
		// 尝试将 []any 转换为 []map[string]any
		tempResults := make([]map[string]any, 0, len(data))
		for _, item := range data {
			if mapItem, ok := item.(map[string]any); ok {
				tempResults = append(tempResults, mapItem)
			} else {
				return nil, fmt.Errorf("'result_data' 数组包含非对象元素")
			}
		}
		results = tempResults
	default:
		return nil, fmt.Errorf("无效的 'result_data' 类型，期望是 JSON 字符串或对象数组")
	}

	if len(results) == 0 {
		utils.DefaultLogger.Info("无需保存空的分析结果", zap.String("connID", connID), zap.String("tableName", uniqueTableName))
		// 可以选择返回成功或一个提示信息
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: `{"success": true, "message": "没有数据需要保存", "table_name": "` + uniqueTableName + `"}`},
			},
		}, nil
	}

	// 2. 动态构造 CREATE TABLE 和 INSERT 语句 (极其小心！)
	//    更好的方式是预定义表结构或使用更安全的 ORM/Query Builder

	// 2a. 推断列名和类型 (基于第一行数据) - 这很脆弱！
	firstRow := results[0]
	var columnNames []string
	var columnDefs []string
	var valuePlaceholders []string
	var insertArgs [][]any // 用于批量插入

	colIndex := 1
	for name, value := range firstRow {
		safeColName := utils.QuoteIdentifier(name) // 清理并引用列名
		pgType := inferPostgresType(value)         // 推断 PG 类型
		if pgType == "" {
			return nil, fmt.Errorf("无法推断列 '%s' 的 PostgreSQL 类型", name)
		}
		columnNames = append(columnNames, safeColName)
		columnDefs = append(columnDefs, fmt.Sprintf("%s %s", safeColName, pgType))
		valuePlaceholders = append(valuePlaceholders, fmt.Sprintf("$%d", colIndex))
		colIndex++
	}

	// 构造 CREATE TABLE 语句 - 确保只在 temp schema 创建
	// 使用 quote_ident 确保表名安全
	// 注意: 这里的 uniqueTableName 已经包含了 "temp." 前缀
	createTableSQL := fmt.Sprintf("CREATE TABLE %s (%s);",
		utils.QuoteIdentifier(uniqueTableName), // "temp"."table_name"
		strings.Join(columnDefs, ", "),
	)

	// 构造 INSERT 语句
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		utils.QuoteIdentifier(uniqueTableName),
		strings.Join(columnNames, ", "),
		strings.Join(valuePlaceholders, ", "),
	)

	// 准备插入数据
	for _, row := range results {
		rowArgs := make([]any, 0, len(columnNames))
		for i := range columnNames {
			// 需要从原始列名获取值，因为 columnNames 已经被 quote 了
			originalColName := strings.Trim(columnNames[i], `"`) // 假设 QuoteIdentifier 加了双引号
			val, exists := row[originalColName]
			if !exists {
				// 理论上不应该发生，因为列是基于第一行推断的
				rowArgs = append(rowArgs, nil) // 或者返回错误
			} else {
				rowArgs = append(rowArgs, val)
			}
		}
		insertArgs = append(insertArgs, rowArgs)
	}

	// 3. 执行数据库操作 (使用读写模式，并且需要事务)
	utils.DefaultLogger.Info("准备向 temp schema 写入数据...", zap.String("connID", connID), zap.String("tableName", uniqueTableName))

	// 这里需要一个能执行多条语句的事务性操作
	// 我们可以通过 dbService 暴露一个 ExecuteTx 方法，或者在这里直接获取连接池操作
	pool, err := h.dbService.GetPool(ctx, connID)
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite}) // 显式读写模式
	if err != nil {
		return nil, fmt.Errorf("开始读写事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // 保证回滚

	// 执行 CREATE TABLE
	utils.DefaultLogger.Debug("执行 CREATE TABLE", zap.String("sql", createTableSQL))
	_, err = tx.Exec(ctx, createTableSQL)
	if err != nil {
		utils.DefaultLogger.Error("创建 temp 表失败", zap.Error(err), zap.String("sql", createTableSQL))
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"success": false, "error": "创建临时表失败: %v"}`, err)},
			},
			IsError: true,
		}, nil
	}

	// 批量执行 INSERT
	utils.DefaultLogger.Debug("准备批量插入数据", zap.Int("rowCount", len(insertArgs)))
	// 使用 pgx 的 Batch 功能提高效率
	batch := &pgx.Batch{}
	for _, args := range insertArgs {
		batch.Queue(insertSQL, args...)
	}
	br := tx.SendBatch(ctx, batch)
	// 检查批量操作的结果
	for i := 0; i < len(insertArgs); i++ {
		_, errExec := br.Exec()
		if errExec != nil {
			closeErr := br.Close() // 必须关闭 batch results
			utils.DefaultLogger.Error("批量插入时发生错误", zap.Error(errExec), zap.Int("rowIndex", i), zap.NamedError("closeErr", closeErr))
			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"success": false, "error": "插入第 %d 行数据失败: %v"}`, i+1, errExec)},
				},
				IsError: true,
			}, nil
		}
	}
	if err := br.Close(); err != nil { // 关闭并检查最终错误
		utils.DefaultLogger.Error("关闭 BatchResults 时发生错误", zap.Error(err))
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"success": false, "error": "完成批量插入时出错: %v"}`, err)},
			},
			IsError: true,
		}, nil
	}

	// 提交事务
	if err := tx.Commit(ctx); err != nil {
		utils.DefaultLogger.Error("提交 temp 表写入事务失败", zap.Error(err))
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"success": false, "error": "提交事务失败: %v"}`, err)},
			},
			IsError: true,
		}, nil
	}

	utils.DefaultLogger.Info("成功将分析结果保存到 temp 表", zap.String("connID", connID), zap.String("tableName", uniqueTableName), zap.Int("rowCount", len(results)))

	// 4. 返回成功结果
	resultData := map[string]any{
		"success":    true,
		"table_name": uniqueTableName,
		"rows_saved": len(results),
	}
	resultBytes, _ := json.Marshal(resultData) // 忽略序列化错误

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{Type: "text", Text: string(resultBytes)},
		},
	}, nil
}

// inferPostgresType 简单地根据 Go 类型推断 PostgreSQL 类型 (非常基础，需要完善)
func inferPostgresType(value any) string {
	switch value.(type) {
	case int, int8, int16, int32, int64:
		return "bigint" // 或者根据范围选择 integer
	case float32:
		return "real"
	case float64:
		return "double precision"
	case bool:
		return "boolean"
	case string:
		// 检查是否像日期时间？需要更复杂的逻辑
		// 默认使用 text
		return "text"
	case time.Time: // 需要导入 time 包
		return "timestamp with time zone"
	case []byte:
		return "bytea"
	// 可以添加对 map[string]any 或 []any -> jsonb 的推断
	case map[string]any, []any:
		return "jsonb"
	default:
		return "" // 未知类型
	}
}

// (确保 utils 包中有 SanitizeIdentifier 和 QuoteIdentifierQualified 函数)
/* 示例 utils/security.go:
package utils

import (
	"regexp"
	"strings"
	"fmt"
)

var nonAlphanumericUnderscore = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

// SanitizeIdentifier 移除标识符中不安全的字符，只保留字母、数字、下划线
func SanitizeIdentifier(identifier string) string {
	return nonAlphanumericUnderscore.ReplaceAllString(identifier, "")
}

// QuoteIdentifier 安全地引用单个 PostgreSQL 标识符 (表名, 列名)
func QuoteIdentifier(identifier string) string {
	// 替换双引号为两个双引号，然后用双引号包围
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// QuoteIdentifierQualified 安全地引用可能带 schema 的标识符 (e.g., "temp"."mytable")
func QuoteIdentifierQualified(qualifiedIdentifier string) string {
    parts := strings.SplitN(qualifiedIdentifier, ".", 2)
    if len(parts) == 2 {
        // 假设是 schema.table
        return fmt.Sprintf("%s.%s", QuoteIdentifier(parts[0]), QuoteIdentifier(parts[1]))
    }
    // 否则认为是单个标识符
    return QuoteIdentifier(qualifiedIdentifier)
}
*/
