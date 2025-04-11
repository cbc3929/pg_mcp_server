package handlers

import (
	"context" // 引入 context 包，因为 Handler 内部需要创建
	"encoding/json"
	"fmt" // 引入 url 包，用于在 Resource Handler 内部解析 URI
	"strings"
	"time" // 用于创建带超时的 Context

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/core/extensions"
	"github.com/cbc3929/pg_mcp_server/internal/core/schemas"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	uritemplate "github.com/yosida95/uritemplate/v3" // 引入 URI 模板解析库
	"go.uber.org/zap"
)

// --- 定义 Tool 输入参数的结构体 (保持不变) ---
type ConnectToolArgs struct {
	ConnectionString string `json:"connection_string"`
}
type DisconnectToolArgs struct {
	ConnID string `json:"conn_id"`
}
type PgQueryToolArgs struct {
	ConnID string `json:"conn_id"`
	Query  string `json:"query"`
	Params []any  `json:"params,omitempty"`
}

// --- 注册函数 ---

// RegisterHandlers 将所有定义的 MCP Tool 和 Resource 处理器注册到服务器。
// 适配 go-mcp 的实际 Handler 签名。
func RegisterHandlers(mcpServer *server.Server, dbService databases.Service, schemaManager schemas.Manager, extManager extensions.Manager) error {
	utils.DefaultLogger.Info("开始注册 MCP Handlers (适配实际签名)...")

	// --- 注册 Tools ---

	// 注册 'connect' 工具
	connectTool, err := protocol.NewTool("connect", "注册数据库连接字符串并返回连接 ID", ConnectToolArgs{})
	if err != nil {
		return fmt.Errorf("创建 'connect' 工具定义失败: %w", err)
	}
	mcpServer.RegisterTool(connectTool, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		// Handler 内部创建 Context
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second) // 15秒超时示例
		defer cancel()

		args := new(ConnectToolArgs)
		// 使用 VerifyAndUnmarshal 解析 RawArguments
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil { // 假设 VerifyAndUnmarshal 在 pkg 包
			utils.DefaultLogger.Error("'connect' 参数解析失败", zap.Error(err), zap.ByteString("rawArgs", request.RawArguments))
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnectionString == "" {
			return nil, fmt.Errorf("缺少 'connection_string' 参数")
		}

		utils.DefaultLogger.Info("处理 'connect' 调用", zap.String("connectionStringPrefix", args.ConnectionString[:min(len(args.ConnectionString), 20)]))

		connID, err := dbService.RegisterConnection(ctx, args.ConnectionString) // 传递创建的 Context
		if err != nil {
			utils.DefaultLogger.Error("注册数据库连接失败", zap.Error(err))
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"error": "注册连接失败: %v"}`, err)}}, IsError: true}, nil
		}

		resultData := map[string]string{"conn_id": connID}
		resultBytes, _ := json.Marshal(resultData)
		return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "application/json", Text: string(resultBytes)}}}, nil
	})
	utils.DefaultLogger.Info("Tool 'connect' 已注册")

	// 注册 'disconnect' 工具
	disconnectTool, err := protocol.NewTool("disconnect", "关闭指定的数据库连接", DisconnectToolArgs{})
	if err != nil {
		return fmt.Errorf("创建 'disconnect' 工具定义失败: %w", err)
	}
	mcpServer.RegisterTool(disconnectTool, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		args := new(DisconnectToolArgs)
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil { // 使用正确的反序列化
			utils.DefaultLogger.Error("'disconnect' 参数解析失败", zap.Error(err), zap.ByteString("rawArgs", request.RawArguments))
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnID == "" {
			return nil, fmt.Errorf("缺少 'conn_id' 参数")
		}

		utils.DefaultLogger.Info("处理 'disconnect' 调用", zap.String("connID", args.ConnID))

		err := dbService.DisconnectConnection(ctx, args.ConnID)
		if err != nil {
			utils.DefaultLogger.Error("断开数据库连接失败", zap.String("connID", args.ConnID), zap.Error(err))
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"success": false, "error": "断开连接失败: %v"}`, err)}}, IsError: true}, nil
		}

		resultData := map[string]bool{"success": true}
		resultBytes, _ := json.Marshal(resultData)
		return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "application/json", Text: string(resultBytes)}}}, nil
	})
	utils.DefaultLogger.Info("Tool 'disconnect' 已注册")

	// 注册 'pg_query' 工具
	pgQueryTool, err := protocol.NewTool("pg_query", "执行只读 SQL 查询", PgQueryToolArgs{})
	if err != nil {
		return fmt.Errorf("创建 'pg_query' 工具定义失败: %w", err)
	}
	mcpServer.RegisterTool(pgQueryTool, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // 查询可能耗时较长
		defer cancel()

		args := new(PgQueryToolArgs)
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil { // 使用正确的反序列化
			utils.DefaultLogger.Error("'pg_query' 参数解析失败", zap.Error(err), zap.ByteString("rawArgs", request.RawArguments))
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnID == "" || args.Query == "" {
			return nil, fmt.Errorf("缺少 'conn_id' 或 'query' 参数")
		}

		utils.DefaultLogger.Info("处理 'pg_query' 调用", zap.String("connID", args.ConnID), zap.String("query", args.Query))

		results, err := dbService.ExecuteQuery(ctx, args.ConnID, true, args.Query, args.Params...)
		if err != nil {
			utils.DefaultLogger.Error("执行 'pg_query' 失败", zap.Error(err))
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"error": "查询执行失败: %v"}`, err)}}, IsError: true}, nil
		}

		resultBytes, err := json.Marshal(results)
		if err != nil {
			return nil, fmt.Errorf("序列化查询结果失败: %w", err)
		}
		return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "application/json", Text: string(resultBytes)}}}, nil
	})
	utils.DefaultLogger.Info("Tool 'pg_query' 已注册")

	// 注册 'pg_explain' 工具
	pgExplainTool, err := protocol.NewTool("pg_explain", "获取 SQL 执行计划", PgQueryToolArgs{})
	if err != nil {
		return fmt.Errorf("创建 'pg_explain' 工具定义失败: %w", err)
	}
	mcpServer.RegisterTool(pgExplainTool, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		args := new(PgQueryToolArgs)
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil { // 使用正确的反序列化
			utils.DefaultLogger.Error("'pg_explain' 参数解析失败", zap.Error(err), zap.ByteString("rawArgs", request.RawArguments))
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnID == "" || args.Query == "" {
			return nil, fmt.Errorf("缺少 'conn_id' 或 'query' 参数")
		}

		utils.DefaultLogger.Info("处理 'pg_explain' 调用", zap.String("connID", args.ConnID), zap.String("query", args.Query))

		explainQuery := "EXPLAIN (FORMAT JSON) " + args.Query
		results, err := dbService.ExecuteQuery(ctx, args.ConnID, true, explainQuery, args.Params...)
		if err != nil {
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"error": "EXPLAIN 执行失败: %v"}`, err)}}, IsError: true}, nil
		}

		// ... (Explain 结果处理逻辑不变) ...
		var explainPlanJSON string
		if len(results) > 0 && results[0] != nil {
			if planField, ok := results[0]["QUERY PLAN"]; ok {
				planBytes, err := json.Marshal(planField)
				if err != nil {
					return nil, fmt.Errorf("序列化 Explain Plan 失败: %w", err)
				}
				explainPlanJSON = string(planBytes)
			} else {
				resultBytes, err := json.Marshal(results)
				if err != nil {
					return nil, fmt.Errorf("序列化原始 Explain 结果失败: %w", err)
				}
				explainPlanJSON = string(resultBytes)
			}
		} else {
			explainPlanJSON = "[]"
		}

		return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "application/json", Text: explainPlanJSON}}}, nil
	})
	utils.DefaultLogger.Info("Tool 'pg_explain' 已注册")

	// --- 注册 Resources (使用 RegisterResourceTemplate 和适配的 Handler 签名) ---

	// 辅助函数：用于从 URI 模板和实际 URI 中提取变量
	extractPathVariables := extractPathVariables

	// 注册数据库完整信息资源模板
	dbInfoTemplate := &protocol.ResourceTemplate{URITemplate: "pgmcp://{conn_id}/", Description: "获取数据库的完整 Schema 信息"}
	err = mcpServer.RegisterResourceTemplate(dbInfoTemplate,
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			// Handler 内部创建 Context
			_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// 从 URI 中解析变量
			params, err := extractPathVariables(dbInfoTemplate.URITemplate, request.URI)
			if err != nil {
				utils.DefaultLogger.Error("解析数据库信息 URI 变量失败", zap.String("uri", request.URI), zap.Error(err))
				return nil, err // 返回错误给框架
			}
			if params == nil { // 不匹配
				return nil, fmt.Errorf("URI '%s' 与模板 '%s' 不匹配", request.URI, dbInfoTemplate.URITemplate)
			}
			connID := params["conn_id"]
			if connID == "" {
				return nil, fmt.Errorf("从 URI '%s' 中未能提取 conn_id", request.URI)
			}

			utils.DefaultLogger.Info("处理数据库信息资源请求", zap.String("connID", connID), zap.String("uri", request.URI))

			dbInfo, found := schemaManager.GetDatabaseInfo()
			if !found {
				return protocol.NewReadResourceResult(nil), nil
			}

			resultBytes, err := json.Marshal(dbInfo)
			if err != nil {
				return nil, fmt.Errorf("序列化数据库信息失败: %w", err)
			}

			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/' 已注册")

	// 注册 Schema 列表资源模板
	schemaListTemplate := &protocol.ResourceTemplate{URITemplate: "pgmcp://{conn_id}/schemas", Description: "列出所有用户 Schema"}
	err = mcpServer.RegisterResourceTemplate(schemaListTemplate,
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			params, err := extractPathVariables(schemaListTemplate.URITemplate, request.URI)
			if err != nil || params == nil {
				return nil, fmt.Errorf("URI '%s' 变量解析失败或不匹配模板 '%s': %w", request.URI, schemaListTemplate.URITemplate, err)
			}
			connID := params["conn_id"]
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}

			utils.DefaultLogger.Info("处理 Schema 列表资源请求", zap.String("connID", connID), zap.String("uri", request.URI))

			dbInfo, found := schemaManager.GetDatabaseInfo()
			if !found {
				return protocol.NewReadResourceResult(nil), nil
			}
			// ... (Schema 列表提取和序列化逻辑不变) ...
			schemaList := make([]map[string]string, 0, len(dbInfo.Schemas))
			for _, s := range dbInfo.Schemas {
				schemaList = append(schemaList, map[string]string{"name": s.Name, "description": s.Description})
			}
			resultBytes, err := json.Marshal(schemaList)
			if err != nil {
				return nil, fmt.Errorf("序列化 Schema 列表失败: %w", err)
			}

			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas' 已注册")

	// 注册 Table 列表资源模板
	tableListTemplate := &protocol.ResourceTemplate{URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables", Description: "列出指定 Schema 下的所有表"}
	err = mcpServer.RegisterResourceTemplate(tableListTemplate,
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			params, err := extractPathVariables(tableListTemplate.URITemplate, request.URI)
			if err != nil || params == nil {
				return nil, fmt.Errorf("URI '%s' 变量解析失败或不匹配模板 '%s': %w", request.URI, tableListTemplate.URITemplate, err)
			}
			connID := params["conn_id"]
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}
			schemaName := params["schema"]
			if schemaName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 schema: %s", request.URI)
			}

			utils.DefaultLogger.Info("处理 Table 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("uri", request.URI))

			schemaInfo, found := schemaManager.GetSchemaInfo(schemaName)
			if !found {
				return protocol.NewReadResourceResult(nil), nil
			}
			// ... (Table 列表提取和序列化逻辑不变) ...
			tableList := make([]map[string]any, 0, len(schemaInfo.Tables))
			for _, t := range schemaInfo.Tables {
				tableList = append(tableList, map[string]any{"name": t.Name, "description": t.Description, "row_count": t.RowCount})
			}
			resultBytes, err := json.Marshal(tableList)
			if err != nil {
				return nil, fmt.Errorf("序列化 Table 列表失败: %w", err)
			}

			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/tables' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/tables' 已注册")

	// 注册 Column 列表资源模板
	columnListTemplate := &protocol.ResourceTemplate{URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables/{table}/columns", Description: "获取指定表的列信息"}
	err = mcpServer.RegisterResourceTemplate(columnListTemplate,
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			params, err := extractPathVariables(columnListTemplate.URITemplate, request.URI)
			if err != nil || params == nil {
				return nil, fmt.Errorf("URI '%s' 变量解析失败或不匹配模板 '%s': %w", request.URI, columnListTemplate.URITemplate, err)
			}
			connID := params["conn_id"]
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}
			schemaName := params["schema"]
			if schemaName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 schema: %s", request.URI)
			}
			tableName := params["table"]
			if tableName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 table: %s", request.URI)
			}

			utils.DefaultLogger.Info("处理 Column 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.String("uri", request.URI))

			tableInfo, found := schemaManager.GetTableInfo(schemaName, tableName)
			if !found {
				return protocol.NewReadResourceResult(nil), nil
			}

			resultBytes, err := json.Marshal(tableInfo.Columns)
			if err != nil {
				return nil, fmt.Errorf("序列化 Column 列表失败: %w", err)
			}

			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/columns' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/columns' 已注册")

	// ... (省略其他 Resource Template 的注册，模式类似：创建 Template 对象，注册闭包 Handler，Handler 内部解析 URI 变量，调用 Manager 获取数据，序列化，返回 ReadResourceResult) ...
	// 例如 Indexes, Constraints, Extensions List, Extension Knowledge, Sample, RowCount

	utils.DefaultLogger.Info("所有 MCP Handlers 注册完成。")
	return nil
}

// min 返回两个整数中较小的一个 (辅助函数)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func extractPathVariables(uriTemplate, actualURI string) (map[string]string, error) {
	tmpl, err := uritemplate.New(uriTemplate)
	if err != nil {
		// 模板本身有问题，这是一个配置错误，应该返回 error
		return nil, fmt.Errorf("解析 URI 模板 '%s' 失败: %w", uriTemplate, err)
	}

	// Match 只返回 *template.Values，如果失败则返回 nil
	values := tmpl.Match(actualURI)

	// 检查是否匹配成功
	if values == nil {
		// URI 与模板不匹配
		return nil, nil // 返回 nil map 表示不匹配
	}

	// 匹配成功，提取变量
	params := make(map[string]string)

	// values.Map() 可以获取一个 map[string]interface{}
	valueMap := values.Map()

	for name, valueInterface := range valueMap {
		// valueInterface 的类型是 uritemplate.Value 的底层类型 (string 或 []string)
		switch v := valueInterface.(type) {
		case string:
			params[name] = v
		case []string:
			// 如果模板变量是列表形式 ({,var*}) 或 ({/var*})
			// 将其转换为逗号分隔的字符串，或者根据需要处理
			params[name] = strings.Join(v, ",")
			utils.DefaultLogger.Debug("URI 模板变量解析为列表", zap.String("key", name), zap.Strings("value", v))
		default:
			// 其他未知类型，尝试用 fmt 转换
			params[name] = fmt.Sprintf("%v", valueInterface)
			utils.DefaultLogger.Warn("URI 模板变量解析出未知类型", zap.String("key", name), zap.Any("value", valueInterface), zap.String("type", fmt.Sprintf("%T", valueInterface)))
		}
	}

	// 如果需要确保模板中定义的所有变量都存在（即使为空），可以这样做：
	// templateVars := tmpl.Varnames // 假设有这个方法，如果没有，需要其他方式获取
	// for _, varName := range templateVars {
	//     if _, exists := params[varName]; !exists {
	//         params[varName] = "" // 或者报错，取决于是否允许缺失
	//     }
	// }
	// **注意:** `uritemplate/v3` 似乎没有直接提供 `Varnames()` 方法。如果必须检查所有模板变量，可能需要解析模板字符串。
	// 但通常 `values.Map()` 返回的已匹配变量足够用了。

	return params, nil // 返回提取的参数 map 和 nil error
}
