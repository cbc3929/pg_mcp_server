package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url" // 引入 url 包
	"strconv"
	"strings" // 引入 strings 包
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/core/extensions"
	"github.com/cbc3929/pg_mcp_server/internal/core/schemas"
	"github.com/cbc3929/pg_mcp_server/internal/utils"

	// 不再需要 uritemplate 库
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
// 使用基本的手动 URI 解析。
func RegisterHandlers(mcpServer *server.Server, dbService databases.Service, schemaManager schemas.Manager, extManager extensions.Manager) error {
	utils.DefaultLogger.Info("开始注册 MCP Handlers (使用手动 URI 解析)...")

	// --- 注册 Tools (这部分逻辑不变) ---
	connectTool, err := protocol.NewTool("connect", "注册数据库连接字符串并返回连接 ID", ConnectToolArgs{})
	if err != nil {
		return fmt.Errorf("创建 'connect' 工具定义失败: %w", err)
	}
	mcpServer.RegisterTool(connectTool, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		args := new(ConnectToolArgs)
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil {
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnectionString == "" {
			return nil, fmt.Errorf("缺少 'connection_string' 参数")
		}
		connID, err := dbService.RegisterConnection(ctx, args.ConnectionString)
		if err != nil {
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"error": "注册连接失败: %v"}`, err)}}, IsError: true}, nil
		}
		resultData := map[string]string{"conn_id": connID}
		resultBytes, _ := json.Marshal(resultData)
		return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "application/json", Text: string(resultBytes)}}}, nil
	})
	utils.DefaultLogger.Info("Tool 'connect' 已注册")

	disconnectTool, err := protocol.NewTool("disconnect", "关闭指定的数据库连接", DisconnectToolArgs{})
	if err != nil {
		return fmt.Errorf("创建 'disconnect' 工具定义失败: %w", err)
	}
	mcpServer.RegisterTool(disconnectTool, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		args := new(DisconnectToolArgs)
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil {
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnID == "" {
			return nil, fmt.Errorf("缺少 'conn_id' 参数")
		}
		err := dbService.DisconnectConnection(ctx, args.ConnID)
		if err != nil {
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"success": false, "error": "断开连接失败: %v"}`, err)}}, IsError: true}, nil
		}
		resultData := map[string]bool{"success": true}
		resultBytes, _ := json.Marshal(resultData)
		return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "application/json", Text: string(resultBytes)}}}, nil
	})
	utils.DefaultLogger.Info("Tool 'disconnect' 已注册")

	pgQueryToolManual := &protocol.Tool{
		Name:        "pg_query",
		Description: "对指定的数据库连接执行一个只读的 SQL 查询",
		InputSchema: protocol.InputSchema{
			Type: protocol.Object, // 使用 Object 常量
			Properties: map[string]*protocol.Property{
				"conn_id": {
					Type:        protocol.String, // 直接使用常量
					Description: "目标数据库的连接 ID",
				},
				"query": {
					Type:        protocol.String,
					Description: "要执行的 SQL 查询语句 (应使用 $1, $2... 作为参数占位符)",
				},
				"params": {
					Type:        protocol.Array, // 类型是数组
					Description: "(可选) 查询语句对应的参数列表 (可以是字符串, 数字, 布尔等)",
					Items: &protocol.Property{
						// 将 Items 的 Type 设置为 String 作为一种妥协。
						// 因为库不支持 "any"，定义为 String 至少能通过 Schema 定义阶段。
						// Handler 中的 JsonUnmarshal 仍能将 JSON 数组解析到 []any。
						// 数据库驱动 pgx 通常也能处理 []any 中的不同类型。
						Type:        protocol.String,
						Description: "数组中的单个参数 (Schema 定义为 string，但接受任意 JSON 类型)",
					},
				},
			},
			Required: []string{"conn_id", "query"},
		},
	}
	mcpServer.RegisterTool(pgQueryToolManual, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		args := new(PgQueryToolArgs)
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil {
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnID == "" || args.Query == "" {
			return nil, fmt.Errorf("缺少 'conn_id' 或 'query' 参数")
		}
		results, err := dbService.ExecuteQuery(ctx, args.ConnID, true, args.Query, args.Params...)
		if err != nil {
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"error": "查询执行失败: %v"}`, err)}}, IsError: true}, nil
		}
		resultBytes, err := json.Marshal(results)
		if err != nil {
			return nil, fmt.Errorf("序列化查询结果失败: %w", err)
		}
		return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "application/json", Text: string(resultBytes)}}}, nil
	})
	utils.DefaultLogger.Info("Tool 'pg_query' 已注册")

	pgExplainToolManual := &protocol.Tool{
		Name:        "pg_explain",
		Description: "获取指定 SQL 查询的 PostgreSQL 执行计划 (EXPLAIN FORMAT JSON)",
		InputSchema: protocol.InputSchema{
			Type: protocol.Object,
			Properties: map[string]*protocol.Property{
				"conn_id": {Type: protocol.String, Description: "目标数据库的连接 ID"},
				"query":   {Type: protocol.String, Description: "要分析的 SQL 查询语句"},
				"params":  {Type: protocol.Array, Description: "(可选) 查询参数列表", Items: &protocol.Property{Type: protocol.String}}, // Items 定义为 String
			},
			Required: []string{"conn_id", "query"},
		},
	}
	mcpServer.RegisterTool(pgExplainToolManual, func(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		args := new(PgQueryToolArgs)
		if err := protocol.VerifyAndUnmarshal(request.RawArguments, args); err != nil {
			return nil, fmt.Errorf("参数解析错误: %w", err)
		}
		if args.ConnID == "" || args.Query == "" {
			return nil, fmt.Errorf("缺少 'conn_id' 或 'query' 参数")
		}
		explainQuery := "EXPLAIN (FORMAT JSON) " + args.Query
		results, err := dbService.ExecuteQuery(ctx, args.ConnID, true, explainQuery, args.Params...)
		if err != nil {
			return &protocol.CallToolResult{Content: []protocol.Content{protocol.TextContent{Type: "text/plain", Text: fmt.Sprintf(`{"error": "EXPLAIN 执行失败: %v"}`, err)}}, IsError: true}, nil
		}
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

	// --- 注册 Resources (使用 RegisterResourceTemplate 和手动解析) ---

	// 注册数据库完整信息资源模板
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/", // 模板仅用于注册标识
			Description: "获取数据库的完整 Schema 信息",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// 1. 解析请求的 URI
			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				utils.DefaultLogger.Error("解析数据库信息请求 URI 失败", zap.String("uri", request.URI), zap.Error(err))
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}

			// 2. 提取变量 (conn_id 在 Host 部分)
			connID := parsedURI.Host
			if connID == "" {
				utils.DefaultLogger.Error("从 URI 中未能提取 conn_id (Host 为空)", zap.String("uri", request.URI))
				return nil, fmt.Errorf("从 URI '%s' 中未能提取 conn_id", request.URI)
			}

			// 3. 检查路径是否匹配 (根路径)
			if parsedURI.Path != "/" && parsedURI.Path != "" { // 允许根路径为 "/" 或空
				utils.DefaultLogger.Warn("数据库信息请求 URI 路径不匹配预期", zap.String("uri", request.URI), zap.String("expectedPath", "/"))
				return nil, fmt.Errorf("请求的 URI '%s' 路径不符合预期", request.URI)
			}

			utils.DefaultLogger.Info("处理数据库信息资源请求", zap.String("connID", connID), zap.String("uri", request.URI))

			// 4. 调用核心逻辑 (不变)
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
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas",
			Description: "列出所有用户 Schema",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}

			// 检查路径
			if parsedURI.Path != "/schemas" {
				return nil, fmt.Errorf("请求的 URI '%s' 路径不符合预期 '/schemas'", request.URI)
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
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables",
			Description: "列出指定 Schema 下的所有表",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}

			// 按路径段提取 schema
			// Path: /schemas/{schema}/tables
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 3 || pathSegments[0] != "schemas" || pathSegments[2] != "tables" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/tables'", request.URI)
			}
			schemaName := pathSegments[1]
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
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables/{table}/columns",
			Description: "获取指定表的列信息",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}

			// Path: /schemas/{schema}/tables/{table}/columns
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 5 || pathSegments[0] != "schemas" || pathSegments[2] != "tables" || pathSegments[4] != "columns" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/tables/{table}/columns'", request.URI)
			}
			schemaName := pathSegments[1]
			if schemaName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 schema: %s", request.URI)
			}
			tableName := pathSegments[3]
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

	// 注册 Index 列表资源模板
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables/{table}/indexes",
			Description: "获取指定表的索引信息",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 5 || pathSegments[0] != "schemas" || pathSegments[2] != "tables" || pathSegments[4] != "indexes" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/tables/{table}/indexes'", request.URI)
			}
			schemaName := pathSegments[1]
			if schemaName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 schema: %s", request.URI)
			}
			tableName := pathSegments[3]
			if tableName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 table: %s", request.URI)
			}

			utils.DefaultLogger.Info("处理 Index 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.String("uri", request.URI))
			tableInfo, found := schemaManager.GetTableInfo(schemaName, tableName)
			if !found {
				return protocol.NewReadResourceResult(nil), nil
			}
			resultBytes, err := json.Marshal(tableInfo.Indexes)
			if err != nil {
				return nil, fmt.Errorf("序列化 Index 列表失败: %w", err)
			}
			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/indexes' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/indexes' 已注册")

	// 注册 Constraint 列表资源模板 (主要返回外键)
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables/{table}/constraints",
			Description: "获取指定表的外键约束信息",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			_, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 5 || pathSegments[0] != "schemas" || pathSegments[2] != "tables" || pathSegments[4] != "constraints" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/tables/{table}/constraints'", request.URI)
			}
			schemaName := pathSegments[1]
			if schemaName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 schema: %s", request.URI)
			}
			tableName := pathSegments[3]
			if tableName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 table: %s", request.URI)
			}

			utils.DefaultLogger.Info("处理 Constraint 列表资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.String("uri", request.URI))
			tableInfo, found := schemaManager.GetTableInfo(schemaName, tableName)
			if !found {
				return protocol.NewReadResourceResult(nil), nil
			}
			resultBytes, err := json.Marshal(tableInfo.ForeignKeys)
			if err != nil {
				return nil, fmt.Errorf("序列化 Constraint 列表失败: %w", err)
			}
			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/constraints' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/constraints' 已注册")

	// 注册 Extension 列表资源模板
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/extensions",
			Description: "列出数据库中实际安装的扩展及其版本",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}
			// schema 变量在路径中但可能不直接用于查询，仅记录
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 3 || pathSegments[0] != "schemas" || pathSegments[2] != "extensions" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/extensions'", request.URI)
			}
			schemaHint := pathSegments[1]

			utils.DefaultLogger.Info("处理已安装扩展列表资源请求", zap.String("connID", connID), zap.String("schemaHint", schemaHint), zap.String("uri", request.URI))
			query := `SELECT e.extname AS name, e.extversion AS version, n.nspname AS schema_installed_in, obj_description(e.oid, 'pg_extension') AS description FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace ORDER BY e.extname;`
			installedExts, err := dbService.ExecuteQuery(ctx, connID, true, query)
			if err != nil {
				return nil, fmt.Errorf("查询已安装扩展失败: %w", err)
			}
			resultList := make([]map[string]any, 0, len(installedExts))
			for _, ext := range installedExts {
				extName, _ := ext["name"].(string)
				_, knowledgeFound := extManager.GetExtensionKnowledge(extName)
				ext["knowledge_available"] = knowledgeFound
				resultList = append(resultList, ext)
			}
			resultBytes, err := json.Marshal(resultList)
			if err != nil {
				return nil, fmt.Errorf("序列化扩展列表失败: %w", err)
			}
			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/extensions' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/extensions' 已注册")

	// 注册获取扩展知识资源模板
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/extensions/{extension}",
			Description: "获取指定扩展的本地知识库内容 (JSON)",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second); defer cancel() // 这个操作很快，不需要长超时
			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			// connID := parsedURI.Host // 可能不需要 connID
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 5 || pathSegments[0] != "schemas" || pathSegments[2] != "extensions" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/extensions/{extension}'", request.URI)
			}
			// schemaHint := pathSegments[1]
			extensionName := pathSegments[3]
			if extensionName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 extension: %s", request.URI)
			}

			utils.DefaultLogger.Info("处理获取扩展知识资源请求", zap.String("extension", extensionName), zap.String("uri", request.URI))
			knowledgeData, found := extManager.GetExtensionKnowledge(extensionName)
			if !found {
				return protocol.NewReadResourceResult(nil), nil
			}
			resultBytes, err := json.MarshalIndent(knowledgeData, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("序列化扩展知识失败: %w", err)
			}
			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/extensions/{extension}' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/extensions/{extension}' 已注册")

	// 注册获取表样本数据的资源模板
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables/{table}/sample",
			Description: "获取指定表的前 N 行样本数据 (?limit=N)",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 5 || pathSegments[0] != "schemas" || pathSegments[2] != "tables" || pathSegments[4] != "sample" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/tables/{table}/sample'", request.URI)
			}
			schemaName := pathSegments[1]
			if schemaName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 schema: %s", request.URI)
			}
			tableName := pathSegments[3]
			if tableName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 table: %s", request.URI)
			}

			// 解析 limit 查询参数
			limitStr := parsedURI.Query().Get("limit")
			limit := 10 // defaultSampleLimit
			if limitStr != "" {
				if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
					limit = parsedLimit
				}
			}

			utils.DefaultLogger.Info("处理表样本数据资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.Int("limit", limit), zap.String("uri", request.URI))
			safeSchema := utils.QuoteIdentifier(schemaName)
			safeTable := utils.QuoteIdentifier(tableName)
			query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT $1", safeSchema, safeTable)
			results, err := dbService.ExecuteQuery(ctx, connID, true, query, limit)
			if err != nil {
				return nil, fmt.Errorf("执行样本数据查询失败: %w", err)
			}
			resultBytes, err := json.Marshal(results)
			if err != nil {
				return nil, fmt.Errorf("序列化样本数据失败: %w", err)
			}
			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/sample' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/sample' 已注册")

	// 注册获取表行数资源模板
	err = mcpServer.RegisterResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "pgmcp://{conn_id}/schemas/{schema}/tables/{table}/rowcount",
			Description: "获取指定表的大致行数",
		},
		func(request *protocol.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			parsedURI, err := url.Parse(request.URI)
			if err != nil {
				return nil, fmt.Errorf("无效的请求 URI: %w", err)
			}
			connID := parsedURI.Host
			if connID == "" {
				return nil, fmt.Errorf("无法从 URI 提取 conn_id: %s", request.URI)
			}
			pathSegments := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
			if len(pathSegments) != 5 || pathSegments[0] != "schemas" || pathSegments[2] != "tables" || pathSegments[4] != "rowcount" {
				return nil, fmt.Errorf("URI '%s' 路径格式不匹配 '/schemas/{schema}/tables/{table}/rowcount'", request.URI)
			}
			schemaName := pathSegments[1]
			if schemaName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 schema: %s", request.URI)
			}
			tableName := pathSegments[3]
			if tableName == "" {
				return nil, fmt.Errorf("无法从 URI 提取 table: %s", request.URI)
			}

			utils.DefaultLogger.Info("处理表行数资源请求", zap.String("connID", connID), zap.String("schema", schemaName), zap.String("table", tableName), zap.String("uri", request.URI))
			query := `SELECT reltuples::bigint AS approximate_row_count FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = 'r'`
			results, err := dbService.ExecuteQuery(ctx, connID, true, query, schemaName, tableName)
			if err != nil {
				return nil, fmt.Errorf("执行行数查询失败: %w", err)
			}
			var rowCount int64 = 0
			if len(results) > 0 {
				if countVal, ok := results[0]["approximate_row_count"]; ok {
					rowCount = utils.DbInt64(countVal)
				}
			}
			resultData := map[string]int64{"approximate_row_count": rowCount}
			resultBytes, err := json.Marshal(resultData)
			if err != nil {
				return nil, fmt.Errorf("序列化行数结果失败: %w", err)
			}
			textContent := protocol.TextResourceContents{URI: request.URI, MimeType: "application/json", Text: string(resultBytes)}
			return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
		})
	if err != nil {
		return fmt.Errorf("注册 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/rowcount' 资源模板失败: %w", err)
	}
	utils.DefaultLogger.Info("Resource Template 'pgmcp://{conn_id}/schemas/{schema}/tables/{table}/rowcount' 已注册")

	utils.DefaultLogger.Info("所有 MCP Handlers 注册完成。")
	return nil
}
