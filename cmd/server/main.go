// cmd/server/main.go
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time" // 引入 time 包

	"github.com/cbc3929/pg_mcp_server/internal/config"
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/core/extensions"
	"github.com/cbc3929/pg_mcp_server/internal/core/schemas"
	"github.com/cbc3929/pg_mcp_server/internal/server"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"go.uber.org/zap"
)

func main() {
	utils.SetupLogger(true)
	// 1. 加载配置
	cfg := config.LoadConfig()

	defer func() { _ = utils.DefaultLogger.Sync() }() // 程序退出前同步日志

	utils.DefaultLogger.Info("应用程序启动...")

	// 3. 创建核心服务
	dbService := databases.NewPgxService(cfg)
	schemaManager := schemas.NewManager(dbService)
	extManager := extensions.NewManager(cfg.ExtensionsDir)

	// 4. 启动时加载数据 (使用后台 Context，不应被信号中断)
	//    需要一个 connID 来加载 Schema，可以临时注册一个配置中的 DB URL
	//    或者修改 LoadSchema 接受连接字符串？这里假设临时注册。
	//    注意：如果启动时无法连接数据库加载 Schema 是致命错误还是可接受？
	//    这里假设是致命错误。

	// --- 获取 Schema 加载所需的 connID ---
	// 假设我们用一个固定的、有权限读取 information_schema 的连接串
	// 这个连接串可以来自配置，或者硬编码（不推荐）
	// schemaLoadConnStr := cfg.SchemaLoadDBURL // 从配置获取
	// **简化处理：这里假设 Connect 调用很快，直接在启动时注册一个**
	// **注意：这要求数据库服务在main函数启动时可用**
	tempCtx, tempCancel := context.WithTimeout(context.Background(), 10*time.Second)                                           // 10秒超时
	schemaLoadConnID, err := dbService.RegisterConnection(tempCtx, "postgres://mcp_user:mcp123456@192.168.2.19:5432/postgres") // !! 替换为实际的可读 Schema 的连接串 !!
	tempCancel()
	if err != nil {
		utils.DefaultLogger.Fatal("启动时注册 Schema 加载连接失败", zap.Error(err))
		return // 使用 Fatal 会自动退出
	}
	utils.DefaultLogger.Info("临时获取 Schema 加载连接 ID", zap.String("connID", schemaLoadConnID))

	// --- 加载 Schema 和扩展知识 ---
	loadCtx, loadCancel := context.WithTimeout(context.Background(), 5*time.Minute) // 5分加载超时
	if err := schemaManager.LoadSchema(loadCtx, schemaLoadConnID); err != nil {
		utils.DefaultLogger.Fatal("加载数据库 Schema 失败", zap.Error(err))
		loadCancel()
		return
	}
	if err := extManager.LoadKnowledge(); err != nil {
		utils.DefaultLogger.Fatal("加载扩展知识失败", zap.Error(err))
		loadCancel()
		return
	}
	loadCancel()

	// --- (可选) 加载完 Schema 后可以断开临时连接 ---
	// disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	// _ = dbService.DisconnectConnection(disconnectCtx, schemaLoadConnID) // 忽略错误
	// disconnectCancel()

	// 5. 创建并配置 MCP 服务器
	mcpServer, err := server.NewMCPServer(cfg, dbService, schemaManager, extManager)
	if err != nil {
		// NewMCPServer 内部已经记录了 Fatal 错误，这里可以直接返回
		return
	}

	// 6. 启动服务器 (阻塞)
	runErrChan := make(chan error, 1)
	go func() {
		runErrChan <- mcpServer.Run()
	}()

	// 7. 监听退出信号，实现优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-runErrChan:
		if err != nil {
			utils.DefaultLogger.Error("MCP 服务器运行提前退出", zap.Error(err))
		} else {
			utils.DefaultLogger.Info("MCP 服务器正常停止。")
		}
	case sig := <-quit:
		utils.DefaultLogger.Info("收到退出信号", zap.String("signal", sig.String()))

		// 创建一个带超时的 Context 用于优雅关闭
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second) // 30秒关闭超时
		defer shutdownCancel()

		// 尝试优雅停止服务器 (如果 Stop 方法有效)
		if err := mcpServer.Stop(shutdownCtx); err != nil {
			utils.DefaultLogger.Error("服务器优雅关闭失败", zap.Error(err))
		} else {
			utils.DefaultLogger.Info("服务器已停止。")
		}
		// 即使 Stop 失败，仍然会继续执行到函数末尾，最终调用 logger.Sync()
	}

	utils.DefaultLogger.Info("应用程序退出。")
}
