package extensions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cbc3929/pg_mcp_server/internal/utils"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3" // 引入 YAML 解析库
)

// Manager 定义了扩展知识管理器的接口
type Manager interface {
	// LoadKnowledge 从配置的目录加载所有扩展知识 YAML 文件并缓存。
	LoadKnowledge() error

	// GetExtensionKnowledge 返回指定扩展名的缓存知识数据。
	// found bool指示是否找到了该扩展的知识。
	GetExtensionKnowledge(extensionName string) (KnowledgeData, bool)
}

// manager 是 ExtensionManager 接口的实现。
type manager struct {
	extensionsDir string                   // 存放 YAML 文件的目录
	cache         map[string]KnowledgeData // 扩展名 -> 解析后的 YAML 数据
	mu            sync.RWMutex             // 保护缓存的读写锁
}

// NewManager 创建一个新的 Extension Manager 实例。
// extensionsDir: 包含扩展知识 YAML 文件的目录路径。
func NewManager(extensionsDir string) Manager {
	utils.DefaultLogger.Info("初始化扩展知识管理器...", zap.String("directory", extensionsDir))
	return &manager{
		extensionsDir: extensionsDir,
		cache:         make(map[string]KnowledgeData),
		// mu 默认零值可用
	}
}

// LoadKnowledge 实现 Manager 接口。
func (m *manager) LoadKnowledge() error {
	utils.DefaultLogger.Info("开始加载扩展知识 YAML 文件...", zap.String("directory", m.extensionsDir))

	m.mu.Lock() // 获取写锁
	defer m.mu.Unlock()

	// 清空旧缓存，确保加载的是最新的
	m.cache = make(map[string]KnowledgeData)

	files, err := os.ReadDir(m.extensionsDir)
	if err != nil {
		// 如果目录不存在或是其他读取错误，记录错误但允许服务器继续运行（无扩展知识）
		utils.DefaultLogger.Error("读取扩展知识目录失败", zap.String("directory", m.extensionsDir), zap.Error(err))
		return fmt.Errorf("读取扩展目录 '%s' 失败: %w", m.extensionsDir, err) // 返回错误，让上层决定是否中止
	}

	loadedCount := 0
	for _, file := range files {
		// 跳过目录和非 YAML 文件
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		if !strings.HasSuffix(fileName, ".yaml") && !strings.HasSuffix(fileName, ".yml") {
			continue
		}

		// 提取扩展名 (文件名去除后缀)
		extensionName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		filePath := filepath.Join(m.extensionsDir, fileName)

		utils.DefaultLogger.Debug("正在加载扩展文件...", zap.String("path", filePath))

		// 读取文件内容
		yamlData, err := os.ReadFile(filePath)
		if err != nil {
			utils.DefaultLogger.Error("读取扩展 YAML 文件失败", zap.String("path", filePath), zap.Error(err))
			continue // 跳过这个文件，继续加载其他的
		}

		// 解析 YAML 内容
		var knowledge KnowledgeData
		err = yaml.Unmarshal(yamlData, &knowledge)
		if err != nil {
			utils.DefaultLogger.Error("解析扩展 YAML 文件失败", zap.String("path", filePath), zap.Error(err))
			continue // 跳过这个文件
		}

		// 存入缓存
		m.cache[extensionName] = knowledge
		loadedCount++
		utils.DefaultLogger.Info("成功加载并缓存扩展知识", zap.String("extension", extensionName), zap.String("file", fileName))
	}

	utils.DefaultLogger.Info("扩展知识加载完成", zap.Int("loadedCount", loadedCount), zap.Int("totalFilesChecked", len(files)))
	return nil
}

// GetExtensionKnowledge 实现 Manager 接口。
func (m *manager) GetExtensionKnowledge(extensionName string) (KnowledgeData, bool) {
	m.mu.RLock() // 获取读锁
	defer m.mu.RUnlock()

	knowledge, found := m.cache[extensionName]
	// 返回浅拷贝，如果需要防止外部修改缓存，应考虑深拷贝
	return knowledge, found
}
