package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/utils" // 引入日志

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"go.uber.org/zap"
)

// QueryHandler 处理查询相关的工具调用。
type QueryHandler struct {
	dbService databases.Service
}

// NewQueryHandler 创建一个新的 QueryHandler。
func NewQueryHandler(dbService databases.Service) *QueryHandler {
	return &QueryHandler{dbService: dbService}
}

// HandlePgQuery 处理 'pg_query' 工具的调用请求。
func (h *QueryHandler) HandlePgQuery(ctx context.Context, req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	utils.DefaultLogger.Info("收到 'pg_query' 工具调用请求")

	// 1. 提取参数
	connID, query, params, err := extractQueryParams(req.Arguments)
	if err != nil {
		utils.DefaultLogger.Error("'pg_query' 请求参数提取失败", zap.Error(err), zap.Any("args", req.Arguments))
		return nil, fmt.Errorf("无效的查询参数: %w", err) // 参数错误，返回 error 给框架
	}

	utils.DefaultLogger.Debug("执行 SQL 查询", zap.String("connID", connID), zap.String("query", query), zap.Any("params", params))

	// 2. 调用数据库服务执行查询 (强制只读)
	results, err := h.dbService.ExecuteQuery(ctx, connID, true, query, params...) // readOnly = true
	if err != nil {
		utils.DefaultLogger.Error("执行 'pg_query' 失败", zap.String("connID", connID), zap.String("query", query), zap.Error(err))
		// 返回业务错误结果
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"error": "查询执行失败: %v"}`, err)},
			},
			IsError: true,
		}, nil
	}

	utils.DefaultLogger.Info("SQL 查询执行成功", zap.String("connID", connID), zap.Int("rowCount", len(results)))

	// 3. 序列化结果为 JSON
	// 注意: 如果结果集很大，一次性序列化所有结果可能消耗大量内存。
	// 未来可以考虑流式返回或分页。目前先返回完整结果。
	resultBytes, err := json.Marshal(results)
	if err != nil {
		utils.DefaultLogger.Error("序列化 'pg_query' 结果失败", zap.String("connID", connID), zap.Error(err))
		return nil, fmt.Errorf("序列化查询结果失败: %w", err)
	}

	// 4. 构建成功的响应结果
	return &protocol.CallToolResult{
		Content: []protocol.Content{
			// 根据 MCP 协议，结果应该是 Content 数组，每个元素代表一部分内容。
			// 如果结果是行列表，可以考虑每行一个 TextContent，或者整个 JSON 作为一个 TextContent。
			// 这里选择将整个 JSON 作为一个 TextContent。
			protocol.TextContent{Type: "text", Text: string(resultBytes)},
		},
	}, nil
}

// HandlePgExplain 处理 'pg_explain' 工具的调用请求。
func (h *QueryHandler) HandlePgExplain(ctx context.Context, req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	utils.DefaultLogger.Info("收到 'pg_explain' 工具调用请求")

	// 1. 提取参数
	connID, query, params, err := extractQueryParams(req.Arguments)
	if err != nil {
		utils.DefaultLogger.Error("'pg_explain' 请求参数提取失败", zap.Error(err), zap.Any("args", req.Arguments))
		return nil, fmt.Errorf("无效的查询参数: %w", err)
	}

	// 2. 构造 EXPLAIN 查询
	explainQuery := "EXPLAIN (FORMAT JSON) " + query
	utils.DefaultLogger.Debug("执行 EXPLAIN 查询", zap.String("connID", connID), zap.String("explainQuery", explainQuery), zap.Any("params", params))

	// 3. 调用数据库服务执行 EXPLAIN (强制只读)
	// EXPLAIN 的结果通常是一个 JSON 对象数组，只有一个元素，该元素包含计划。
	results, err := h.dbService.ExecuteQuery(ctx, connID, true, explainQuery, params...) // readOnly = true
	if err != nil {
		utils.DefaultLogger.Error("执行 'pg_explain' 失败", zap.String("connID", connID), zap.String("query", query), zap.Error(err))
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"error": "EXPLAIN 执行失败: %v"}`, err)},
			},
			IsError: true,
		}, nil
	}

	utils.DefaultLogger.Info("EXPLAIN 查询执行成功", zap.String("connID", connID))

	// 4. EXPLAIN 的结果通常是一个包含单个 JSON 对象的数组
	var explainPlanJSON string
	if len(results) > 0 && results[0] != nil {
		// 提取 'QUERY PLAN' 字段的内容，它本身就是 JSON
		if planField, ok := results[0]["QUERY PLAN"]; ok {
			// planField 理论上应该已经是 map[string]any 或 []any (pgx 会尝试解析 JSON)
			planBytes, err := json.Marshal(planField) // 重新序列化，确保是标准 JSON 字符串
			if err != nil {
				utils.DefaultLogger.Error("序列化 Explain Plan 失败", zap.String("connID", connID), zap.Error(err))
				return nil, fmt.Errorf("序列化 Explain Plan 失败: %w", err)
			}
			explainPlanJSON = string(planBytes)
		} else {
			utils.DefaultLogger.Warn("EXPLAIN 结果中未找到 'QUERY PLAN' 字段", zap.String("connID", connID))
			// 可以选择返回整个原始结果的 JSON
			resultBytes, err := json.Marshal(results)
			if err != nil {
				utils.DefaultLogger.Error("序列化 'pg_explain' 原始结果失败", zap.String("connID", connID), zap.Error(err))
				return nil, fmt.Errorf("序列化原始 Explain 结果失败: %w", err)
			}
			explainPlanJSON = string(resultBytes)
		}
	} else {
		utils.DefaultLogger.Warn("EXPLAIN 查询未返回有效结果", zap.String("connID", connID))
		explainPlanJSON = "[]" // 返回空 JSON 数组
	}

	// 5. 构建成功的响应结果
	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{Type: "text", Text: explainPlanJSON},
		},
	}, nil
}

// extractQueryParams 从工具请求参数中提取 conn_id, query 和 params。
func extractQueryParams(args map[string]any) (connID, query string, params []any, err error) {
	// 提取 conn_id
	connIDVal, ok := args["conn_id"]
	if !ok {
		err = fmt.Errorf("缺少 'conn_id' 参数")
		return
	}
	connID, ok = connIDVal.(string)
	if !ok || connID == "" {
		err = fmt.Errorf("无效的 'conn_id' 参数类型或值为空")
		return
	}

	// 提取 query
	queryVal, ok := args["query"]
	if !ok {
		err = fmt.Errorf("缺少 'query' 参数")
		return
	}
	query, ok = queryVal.(string)
	if !ok || query == "" {
		err = fmt.Errorf("无效的 'query' 参数类型或值为空")
		return
	}

	// 提取 params (可选)
	paramsVal, ok := args["params"]
	if !ok {
		params = []any{} // 没有 params 参数，视为空列表
		return
	}

	// params 应该是 []any 类型
	params, ok = paramsVal.([]any)
	if !ok {
		// 尝试看看是不是 JSON 字符串表示的数组
		if paramsStr, isStr := paramsVal.(string); isStr {
			errUnmarshal := json.Unmarshal([]byte(paramsStr), &params)
			if errUnmarshal != nil {
				err = fmt.Errorf("无效的 'params' 参数类型，期望是数组，但提供了 %T (尝试解析字符串也失败: %v)", paramsVal, errUnmarshal)
				return
			}
			// 解析成功
			return
		}

		err = fmt.Errorf("无效的 'params' 参数类型，期望是数组，但提供了 %T", paramsVal)
		return
	}

	return // 成功返回
}
