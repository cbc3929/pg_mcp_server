package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url" // 用于解析 URI 参数

	// coreschema  // 使用别名避免冲突

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	coreschema "github.com/cbc3929/pg_mcp_server/internal/core/schemas"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"go.uber.org/zap"
)

// SchemaHandler 处理 Schema 相关的资源请求。
type SchemaHandler struct {
	schemaManager coreschema.Manager
}

// NewSchemaHandler 创建一个新的 SchemaHandler。
func NewSchemaHandler(schemaManager coreschema.Manager) *SchemaHandler {
	return &SchemaHandler{schemaManager: schemaManager}
}

// HandleDatabaseInfo 处理获取数据库完整信息的请求。
func (h *SchemaHandler) HandleDatabaseInfo(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"] // 从路径参数中获取 conn_id
	utils.DefaultLogger.Info("收到数据库完整信息资源请求", zap.String("connID", connID), zap.String("uri", uri.String()))

	dbInfo, found := h.schemaManager.GetDatabaseInfo()
	if !found {
		utils.DefaultLogger.Warn("数据库 Schema 缓存未找到或为空", zap.String("connID", connID))
		// 可以返回 404 Not Found 错误，或者一个空的结果
		// go-mcp 库似乎没有直接映射 HTTP 状态码，这里返回空内容
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{}}, nil
	}

	// 序列化为 JSON
	resultBytes, err := json.Marshal(dbInfo)
	if err != nil {
		utils.DefaultLogger.Error("序列化数据库信息失败", zap.String("connID", connID), zap.Error(err))
		return nil, fmt.Errorf("序列化数据库信息失败: %w", err)
	}
	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}

// HandleListSchemas 处理列出所有 Schema 的请求。
func (h *SchemaHandler) HandleListSchemas(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	utils.DefaultLogger.Info("收到 Schema 列表资源请求", zap.String("connID", connID), zap.String("uri", uri.String()))

	dbInfo, found := h.schemaManager.GetDatabaseInfo()
	if !found {
		utils.DefaultLogger.Warn("数据库 Schema 缓存未找到或为空 (for listing schemas)", zap.String("connID", connID))
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{}}, nil
	}

	// 只提取 Schema 名称和描述
	schemaList := make([]map[string]string, 0, len(dbInfo.Schemas))
	for _, s := range dbInfo.Schemas {
		schemaList = append(schemaList, map[string]string{
			"name":        s.Name,
			"description": s.Description,
		})
	}

	resultBytes, err := json.Marshal(schemaList)
	if err != nil {
		utils.DefaultLogger.Error("序列化 Schema 列表失败", zap.String("connID", connID), zap.Error(err))
		return nil, fmt.Errorf("序列化 Schema 列表失败: %w", err)
	}
	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil

}

// HandleListTables 处理列出指定 Schema 下所有表的请求。
func (h *SchemaHandler) HandleListTables(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	schemaName := params["schema"] // 从路径参数中获取 schema
	utils.DefaultLogger.Info("收到 Table 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("uri", uri.String()))

	schemaInfo, found := h.schemaManager.GetSchemaInfo(schemaName)
	if !found {
		utils.DefaultLogger.Warn("请求的 Schema 未在缓存中找到", zap.String("connID", connID), zap.String("schema", schemaName))
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{}}, nil // 返回空
	}

	// 只提取 Table 名称、描述和行数
	tableList := make([]map[string]any, 0, len(schemaInfo.Tables))
	for _, t := range schemaInfo.Tables {
		tableList = append(tableList, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"row_count":   t.RowCount,
		})
	}

	resultBytes, err := json.Marshal(tableList)
	if err != nil {
		utils.DefaultLogger.Error("序列化 Table 列表失败", zap.String("connID", connID), zap.String("schema", schemaName), zap.Error(err))
		return nil, fmt.Errorf("序列化 Table 列表失败: %w", err)
	}
	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil

	// return &protocol.ReadResourceResult{
	// 	Content: []protocol.Content{
	// 		protocol.TextContent{Type: "text/json", Text: string(resultBytes)},
	// 	},
	// }, nil
}

// HandleGetColumns 处理获取指定表所有列信息的请求。
func (h *SchemaHandler) HandleGetColumns(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	schemaName := params["schema"]
	tableName := params["table"] // 从路径参数中获取 table
	utils.DefaultLogger.Info("收到 Column 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.String("uri", uri.String()))

	tableInfo, found := h.schemaManager.GetTableInfo(schemaName, tableName)
	if !found {
		utils.DefaultLogger.Warn("请求的 Table 未在缓存中找到", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName))
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{}}, nil // 返回空
	}

	// 列信息已经在 tableInfo.Columns 中
	resultBytes, err := json.Marshal(tableInfo.Columns)
	if err != nil {
		utils.DefaultLogger.Error("序列化 Column 列表失败", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Error(err))
		return nil, fmt.Errorf("序列化 Column 列表失败: %w", err)
	}
	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}

// HandleGetIndexes 处理获取指定表所有索引信息的请求。
func (h *SchemaHandler) HandleGetIndexes(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	schemaName := params["schema"]
	tableName := params["table"]
	utils.DefaultLogger.Info("收到 Index 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.String("uri", uri.String()))

	tableInfo, found := h.schemaManager.GetTableInfo(schemaName, tableName)
	if !found {
		utils.DefaultLogger.Warn("请求的 Table 未在缓存中找到 (for indexes)", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName))
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{}}, nil
	}

	resultBytes, err := json.Marshal(tableInfo.Indexes) // 直接序列化缓存的索引信息
	if err != nil {
		utils.DefaultLogger.Error("序列化 Index 列表失败", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Error(err))
		return nil, fmt.Errorf("序列化 Index 列表失败: %w", err)
	}

	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}

// HandleGetConstraints 处理获取指定表所有约束信息的请求。
func (h *SchemaHandler) HandleGetConstraints(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	schemaName := params["schema"]
	tableName := params["table"]
	utils.DefaultLogger.Info("收到 Constraint 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.String("uri", uri.String()))

	tableInfo, found := h.schemaManager.GetTableInfo(schemaName, tableName)
	if !found {
		utils.DefaultLogger.Warn("请求的 Table 未在缓存中找到 (for constraints)", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName))
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{}}, nil
	}

	// 外键已经在 tableInfo.ForeignKeys 中，还需要其他约束信息吗？
	// SchemaManager 在加载列时查询了所有约束，但只把 PK/UNIQUE 加到了列上。
	// 如果需要返回所有约束（包括 CHECK 等），需要 SchemaManager 缓存完整的约束列表，
	// 或者在这里重新查询一次约束（不推荐）。
	// 当前实现只返回缓存的外键信息。
	resultBytes, err := json.Marshal(tableInfo.ForeignKeys) // 仅序列化外键信息
	if err != nil {
		utils.DefaultLogger.Error("序列化 Constraint (ForeignKeys) 列表失败", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Error(err))
		return nil, fmt.Errorf("序列化 Constraint 列表失败: %w", err)
	}

	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}
