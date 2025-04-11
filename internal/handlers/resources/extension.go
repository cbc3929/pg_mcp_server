package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/core/extensions"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"go.uber.org/zap"
)

// ExtensionHandler 处理扩展相关的资源请求。
type ExtensionHandler struct {
	dbService  databases.Service  // 用于查询实际安装的扩展
	extManager extensions.Manager // 用于获取缓存的扩展知识
}

// NewExtensionHandler 创建一个新的 ExtensionHandler。
func NewExtensionHandler(dbService databases.Service, extManager extensions.Manager) *ExtensionHandler {
	return &ExtensionHandler{
		dbService:  dbService,
		extManager: extManager,
	}
}

// HandleListExtensions 处理列出指定 Schema 下实际安装的扩展。
func (h *ExtensionHandler) HandleListExtensions(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"]
	schemaName := params["schema"] // 这个 schema 参数在这里可能不是必须的，因为 pg_extension 是全局的
	utils.DefaultLogger.Info("收到已安装扩展列表资源请求", zap.String("connID", connID), zap.String("schemaHint", schemaName), zap.String("uri", uri.String()))

	// 查询实际安装的扩展
	// 注意：pg_extension 通常关联到创建它的 schema，但也可能被重定位。
	// 一个更通用的查询是查找所有扩展，无论在哪个 schema。
	query := `
        SELECT
            e.extname AS name,
            e.extversion AS version,
            n.nspname AS schema_installed_in, -- 扩展对象所在的 Schema
            obj_description(e.oid, 'pg_extension') AS description
        FROM
            pg_extension e
        JOIN
            pg_namespace n ON n.oid = e.extnamespace
        ORDER BY
            e.extname;
    `
	// 使用只读模式查询
	installedExts, err := h.dbService.ExecuteQuery(ctx, connID, true, query)
	if err != nil {
		utils.DefaultLogger.Error("查询已安装扩展失败", zap.String("connID", connID), zap.Error(err))
		// 可以返回错误或空列表
		return nil, fmt.Errorf("查询已安装扩展失败: %w", err)
	}

	// 检查每个扩展是否有对应的本地知识缓存
	resultList := make([]map[string]any, 0, len(installedExts))
	for _, ext := range installedExts {
		extName, _ := ext["name"].(string)
		_, knowledgeFound := h.extManager.GetExtensionKnowledge(extName)
		ext["knowledge_available"] = knowledgeFound // 添加标志
		resultList = append(resultList, ext)
	}

	// 序列化结果
	resultBytes, err := json.Marshal(resultList)
	if err != nil {
		utils.DefaultLogger.Error("序列化已安装扩展列表失败", zap.String("connID", connID), zap.Error(err))
		return nil, fmt.Errorf("序列化扩展列表失败: %w", err)
	}

	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "text/json",         // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}

// HandleGetExtensionKnowledge 处理获取特定扩展的缓存知识（YAML 内容）。
func (h *ExtensionHandler) HandleGetExtensionKnowledge(ctx context.Context, uri *url.URL, params map[string]string) (*protocol.ReadResourceResult, error) {
	connID := params["conn_id"] // connID 可能不是必需的，因为知识是本地缓存的，但保留以匹配 URI
	// schemaName := params["schema"] // schema 参数在这里也可能不需要
	extensionName := params["extension"] // 从路径参数获取扩展名
	utils.DefaultLogger.Info("收到获取扩展知识资源请求", zap.String("connID", connID), zap.String("extension", extensionName), zap.String("uri", uri.String()))

	knowledgeData, found := h.extManager.GetExtensionKnowledge(extensionName)
	if !found {
		utils.DefaultLogger.Warn("请求的扩展知识未在缓存中找到", zap.String("connID", connID), zap.String("extension", extensionName))
		// 返回空结果表示未找到
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{}}, nil
	}

	// 将缓存的 map[string]any 序列化回 JSON 字符串
	// 注意：这里返回的是 JSON 格式，即使原始文件是 YAML。如果需要原始 YAML，需要额外存储或处理。
	resultBytes, err := json.MarshalIndent(knowledgeData, "", "  ") // 使用缩进美化输出
	if err != nil {
		utils.DefaultLogger.Error("序列化扩展知识失败", zap.String("connID", connID), zap.String("extension", extensionName), zap.Error(err))
		return nil, fmt.Errorf("序列化扩展知识失败: %w", err)
	}

	resourceURI := uri.String()
	textContent := protocol.TextResourceContents{
		URI:      resourceURI,         // 设置请求的 URI
		MimeType: "application/json",  // 设置正确的 MIME 类型
		Text:     string(resultBytes), // 设置 JSON 字符串内容
	}
	return protocol.NewReadResourceResult([]protocol.ResourceContents{textContent}), nil
}
