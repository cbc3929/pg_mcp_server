package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv" // 用于解析 limit 参数

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"go.uber.org/zap"
)

const defaultSampleLimit = 10 // 默认样本大小

// DataHandler 处理数据相关的资源请求 (样本, 行数)。
type DataHandler struct {
	dbService databases.Service
}

// NewDataHandler 创建一个新的 DataHandler。
func NewDataHandler(dbService databases.Service) *DataHandler {
	return &DataHandler{dbService: dbService}
}

// HandleSampleData 处理获取表样本数据的请求。
func (h *DataHandler) HandleSampleData(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	schemaName := params["schema"]
	tableName := params["table"]
	utils.DefaultLogger.Info("收到表样本数据资源请求",
		zap.String("connID", connID),
		zap.String("schema", schemaName),
		zap.String("table", tableName),
		zap.String("uri", uri.String()),
	)

	// 获取可选的 limit 参数
	limitStr := uri.Query().Get("limit") // 从查询参数获取 limit
	limit := defaultSampleLimit
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err == nil && parsedLimit > 0 {
			limit = parsedLimit
			// 可以考虑设置一个最大 limit 防止滥用
			// const maxSampleLimit = 100
			// if limit > maxSampleLimit { limit = maxSampleLimit }
		} else {
			utils.DefaultLogger.Warn("无效的 limit 查询参数，将使用默认值", zap.String("limitStr", limitStr), zap.Int("default", defaultSampleLimit))
		}
	}

	// 安全地引用标识符
	safeSchema := utils.QuoteIdentifier(schemaName)
	safeTable := utils.QuoteIdentifier(tableName)
	if safeSchema == "" || safeTable == "" {
		utils.DefaultLogger.Error("无效的 schema 或 table 名称", zap.String("schema", schemaName), zap.String("table", tableName))
		return nil, fmt.Errorf("无效的 schema 或 table 名称")
	}

	// 构造查询语句
	// 注意：SELECT * 可能返回大量列或不受支持的类型。更健壮的方式是先获取列名。
	// 但为了简单起见，先用 SELECT *。
	query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT $1", safeSchema, safeTable)
	utils.DefaultLogger.Debug("执行样本数据查询", zap.String("connID", connID), zap.String("query", query), zap.Int("limit", limit))

	// 执行查询 (只读)
	results, err := h.dbService.ExecuteQuery(ctx, connID, true, query, limit)
	if err != nil {
		utils.DefaultLogger.Error("执行样本数据查询失败", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Error(err))
		return nil, fmt.Errorf("执行样本数据查询失败: %w", err)
	}

	utils.DefaultLogger.Info("成功获取样本数据", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Int("rowCount", len(results)))

	// 序列化结果
	resultBytes, err := json.Marshal(results)
	if err != nil {
		utils.DefaultLogger.Error("序列化样本数据失败", zap.String("connID", connID), zap.Error(err))
		return nil, fmt.Errorf("序列化样本数据失败: %w", err)
	}

	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}

// HandleRowCount 处理获取表大致行数的请求。
func (h *DataHandler) HandleRowCount(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	schemaName := params["schema"]
	tableName := params["table"]
	utils.DefaultLogger.Info("收到表行数资源请求",
		zap.String("connID", connID),
		zap.String("schema", schemaName),
		zap.String("table", tableName),
		zap.String("uri", uri.String()),
	)

	// 使用之前 Schema Manager 加载时用的查询（从 pg_class 获取大致行数）
	query := `
        SELECT reltuples::bigint AS approximate_row_count
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = 'r'
    `
	utils.DefaultLogger.Debug("执行行数查询", zap.String("connID", connID), zap.String("query", query), zap.String("schema", schemaName), zap.String("table", tableName))

	// 执行查询 (只读)
	results, err := h.dbService.ExecuteQuery(ctx, connID, true, query, schemaName, tableName)
	if err != nil {
		utils.DefaultLogger.Error("执行行数查询失败", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Error(err))
		return nil, fmt.Errorf("执行行数查询失败: %w", err)
	}

	var rowCount int64 = 0 // 默认值为 0
	if len(results) > 0 {
		if countVal, ok := results[0]["approximate_row_count"]; ok {
			rowCount = utils.DbInt64(countVal) // 使用之前的辅助函数处理类型
		} else {
			utils.DefaultLogger.Warn("行数查询结果中未找到 'approximate_row_count' 字段", zap.String("connID", connID))
		}
	} else {
		utils.DefaultLogger.Warn("行数查询未返回任何结果 (表可能不存在或非普通表?)", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName))
	}

	utils.DefaultLogger.Info("成功获取大致行数", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Int64("rowCount", rowCount))

	// 构建结果
	resultData := map[string]int64{"approximate_row_count": rowCount}
	resultBytes, err := json.Marshal(resultData)
	if err != nil {
		utils.DefaultLogger.Error("序列化行数结果失败", zap.String("connID", connID), zap.Error(err))
		return nil, fmt.Errorf("序列化行数结果失败: %w", err)
	}

	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}
