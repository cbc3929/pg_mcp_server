package server

import (
	"context"
	"fmt"

	// 引入日志

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	mcpserver "github.com/ThinkInAIXYZ/go-mcp/server" // 使用别名避免与包名冲突
	"github.com/ThinkInAIXYZ/go-mcp/transport"
	"github.com/cbc3929/pg_mcp_server/internal/config"
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/core/extensions"
	"github.com/cbc3929/pg_mcp_server/internal/core/schemas"
	"github.com/cbc3929/pg_mcp_server/internal/handlers"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"go.uber.org/zap"
)

// MCPServer 包装了 go-mcp 服务器实例和其依赖。
type MCPServer struct {
	config        *config.Config
	mcpServer     *mcpserver.Server
	dbService     databases.Service
	schemaManager schemas.Manager
	extManager    extensions.Manager
}

// NewMCPServer 创建、配置并返回一个新的 MCPServer 实例。
// 它初始化 MCP 服务器，设置传输方式，并注册所有的 Handlers。
func NewMCPServer(
	cfg *config.Config,
	dbService databases.Service,
	schemaManager schemas.Manager,
	extManager extensions.Manager,
) (*MCPServer, error) {
	utils.DefaultLogger.Info("正在创建 MCP 服务器实例...")

	// 注意：这里假设 transport.NewSSEServerTransport 接受 net.Listener
	// 如果它只接受地址字符串，则直接传入 cfg.ServerAddr
	// 需要根据 go-mcp 的实际 API 调整
	// 假设它接受 Listener:
	transportLayer, err := transport.NewSSEServerTransport(cfg.ServerAddr)
	if err != nil {
		utils.DefaultLogger.Fatal("创建 SSE 传输层失败", zap.String("address", cfg.ServerAddr), zap.Error(err))
		return nil, fmt.Errorf("创建 SSE 传输层失败: %w", err)
	}
	utils.DefaultLogger.Info("SSE 传输层已创建", zap.String("configuredAddress", cfg.ServerAddr))
	// 或者，如果它接受地址字符串:
	// transportLayer, err := transport.NewSSEServerTransport(cfg.ServerAddr)

	// 2. 创建 MCP 服务器实例
	//    可以传递服务器信息等选项
	mcpServerInstance, err := mcpserver.NewServer(transportLayer,
		mcpserver.WithServerInfo(protocol.Implementation{ // 使用 mcpserver.Implementation
			Name:    "pg-mcp-server-go",
			Version: "0.1.0", // 或者从配置/版本文件读取
		}),
		// 这里可以添加其他 WithXXX 选项，例如日志记录器等，如果库支持的话
	)
	if err != nil {
		utils.DefaultLogger.Fatal("创建 MCP 服务器失败", zap.Error(err))
		return nil, fmt.Errorf("创建 MCP 服务器失败: %w", err)
	}
	utils.DefaultLogger.Info("MCP 服务器核心实例已创建")

	// 3. 注册 Handlers
	//    将核心服务和管理器传递给注册函数
	if err := handlers.RegisterHandlers(mcpServerInstance, dbService, schemaManager, extManager); err != nil {
		utils.DefaultLogger.Fatal("注册 MCP Handlers 失败", zap.Error(err))
		return nil, fmt.Errorf("注册 MCP Handlers 失败: %w", err)
	}

	// 4. 组装 MCPServer 结构
	server := &MCPServer{
		config:        cfg,
		mcpServer:     mcpServerInstance,
		dbService:     dbService,
		schemaManager: schemaManager, // 保留引用，虽然注册后主要由 Handler 使用
		extManager:    extManager,
	}

	utils.DefaultLogger.Info("MCP 服务器初始化完成，准备运行。")
	return server, nil
}

// Run 启动 MCP 服务器并开始监听连接。
// 这是一个阻塞操作，直到服务器停止或发生错误。
func (s *MCPServer) Run() error {
	utils.DefaultLogger.Info("启动 MCP 服务器运行...", zap.String("address", s.config.ServerAddr))
	err := s.mcpServer.Run() // 调用 go-mcp 的 Run 方法
	if err != nil {
		utils.DefaultLogger.Error("MCP 服务器运行出错", zap.Error(err))
	} else {
		utils.DefaultLogger.Info("MCP 服务器已停止。")
	}
	return err // 将 Run 的错误返回给调用者 (main)
}

// Stop 优雅地停止 MCP 服务器 (如果 go-mcp 库提供了 Stop 方法)。
// 注意: 需要检查 go-mcp/server.Server 是否有 Stop 或 Shutdown 方法。
// 如果没有，可能需要通过取消传递给 Run 的 Context 来停止。
// 这是一个示例，实际实现依赖于库。
func (s *MCPServer) Stop(ctx context.Context) error {
	utils.DefaultLogger.Info("正在请求停止 MCP 服务器...")
	// 假设 mcpServer 有一个 Stop 方法
	// if hasattr(s.mcpServer, "Stop"):
	//     return s.mcpServer.Stop(ctx)
	// else:
	//     utils.DefaultLogger.Warn("MCP 服务器实例没有提供 Stop 方法。")
	//     return nil

	// 如果没有 Stop 方法，可能需要在 Run 之前设置可取消的 Context
	// 并在这里调用 cancel() 函数。Run 方法需要支持 Context 取消。
	// 目前 go-mcp 的 Run 可能是阻塞的，不一定支持 context 取消。

	// 暂时假设没有 Stop 方法或无法直接停止 Run
	utils.DefaultLogger.Warn("go-mcp 服务器可能没有提供优雅停止的方法，将直接退出。")
	// 可以在这里添加关闭数据库连接池的逻辑，作为最后的清理
	if err := s.dbService.CloseAll(ctx); err != nil {
		utils.DefaultLogger.Error("关闭数据库连接池时出错", zap.Error(err))
	}

	return nil // 或者返回一个表示无法停止的错误
}
