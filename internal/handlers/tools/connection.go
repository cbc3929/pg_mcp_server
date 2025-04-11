package tools

import (
	"context"
	"encoding/json"
	"fmt"

	// 引入数据库服务接口
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/utils" // 引入日志

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"go.uber.org/zap"
)

// ConnectionHandler 处理连接相关的工具调用。
type ConnectionHandler struct {
	dbService databases.Service
}

// NewConnectionHandler 创建一个新的 ConnectionHandler。
func NewConnectionHandler(dbService databases.Service) *ConnectionHandler {
	return &ConnectionHandler{dbService: dbService}
}

// HandleConnect 处理 'connect' 工具的调用请求。
func (h *ConnectionHandler) HandleConnect(ctx context.Context, req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	utils.DefaultLogger.Info("收到 'connect' 工具调用请求")

	// 1. 从请求参数中提取 connection_string
	connString, ok := req.Arguments["connection_string"].(string)
	if !ok || connString == "" {
		utils.DefaultLogger.Error("'connect' 请求缺少或无效的 'connection_string' 参数", zap.Any("args", req.Arguments))
		return nil, fmt.Errorf("缺少或无效的 'connection_string' 参数") // 返回错误给框架
	}

	// 2. 调用数据库服务注册连接
	connID, err := h.dbService.RegisterConnection(ctx, connString)
	if err != nil {
		utils.DefaultLogger.Error("注册数据库连接失败", zap.String("connStringPrefix", connString[:min(len(connString), 20)]), zap.Error(err))
		// 返回包含错误信息的 CallToolResult
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"error": "注册连接失败: %v"}`, err)},
			},
			IsError: true,
		}, nil // 对于 Tool 调用，即使业务出错，通常也返回 nil error 给框架，错误信息放在 Result 里
	}

	utils.DefaultLogger.Info("数据库连接注册成功", zap.String("connID", connID))

	// 3. 构建成功的响应结果
	resultData := map[string]string{"conn_id": connID}
	resultBytes, err := json.Marshal(resultData)
	if err != nil {
		utils.DefaultLogger.Error("序列化 'connect' 成功响应失败", zap.String("connID", connID), zap.Error(err))
		// 序列化失败是一个内部错误，可以返回 error 给框架
		return nil, fmt.Errorf("序列化响应失败: %w", err)
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{Type: "text", Text: string(resultBytes)},
		},
	}, nil
}

// HandleDisconnect 处理 'disconnect' 工具的调用请求。
func (h *ConnectionHandler) HandleDisconnect(ctx context.Context, req *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	utils.DefaultLogger.Info("收到 'disconnect' 工具调用请求")

	// 1. 提取 conn_id
	connID, ok := req.Arguments["conn_id"].(string)
	if !ok || connID == "" {
		utils.DefaultLogger.Error("'disconnect' 请求缺少或无效的 'conn_id' 参数", zap.Any("args", req.Arguments))
		return nil, fmt.Errorf("缺少或无效的 'conn_id' 参数")
	}

	// 2. 调用数据库服务断开连接
	err := h.dbService.DisconnectConnection(ctx, connID)
	if err != nil {
		utils.DefaultLogger.Error("断开数据库连接失败", zap.String("connID", connID), zap.Error(err))
		// 返回业务错误结果
		return &protocol.CallToolResult{
			Content: []protocol.Content{
				protocol.TextContent{Type: "text", Text: fmt.Sprintf(`{"success": false, "error": "断开连接失败: %v"}`, err)},
			},
			IsError: true,
		}, nil
	}

	utils.DefaultLogger.Info("数据库连接断开成功", zap.String("connID", connID))

	// 3. 构建成功的响应结果
	resultData := map[string]bool{"success": true}
	resultBytes, err := json.Marshal(resultData)
	if err != nil {
		utils.DefaultLogger.Error("序列化 'disconnect' 成功响应失败", zap.String("connID", connID), zap.Error(err))
		return nil, fmt.Errorf("序列化响应失败: %w", err)
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{Type: "text", Text: string(resultBytes)},
		},
	}, nil
}

// min 返回两个整数中较小的一个 (辅助函数)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
