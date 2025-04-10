package databases

import (
	"context"
	"errors"
	"fmt"
	"net/url" // 用于解析连接字符串，确保格式正确
	"strings" // 字符串操作
	"sync"    // 用于并发控制 (Mutex)
	"time"

	"github.com/cbc3929/pg_mcp_server/internal/config"
	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"github.com/jackc/pgx/v5/pgxpool" // pgx 连接池
	"go.uber.org/zap"
)

// pgxService 是 DatabaseService 接口的 pgx 实现。
// 它管理连接池和 connID 映射。
type pgxService struct {
	config     *config.Config           // 应用配置
	connMap    map[string]string        // connID -> connectionString 映射
	reverseMap map[string]string        // connectionString -> connID 映射
	pools      map[string]*pgxpool.Pool // connID -> pgxpool.Pool 映射
	mapMutex   sync.RWMutex             // 保护 connMap 和 reverseMap 的读写锁
	poolMutex  sync.Mutex               // 保护 pools 映射的互斥锁 (主要用于创建/删除pool)
}

// NewPgxService 创建一个新的 pgxService 实例。
func NewPgxService(cfg *config.Config) Service {
	utils.DefaultLogger.Info("初始化 Pgx 数据库服务...")
	return &pgxService{
		config:     cfg,
		connMap:    make(map[string]string),
		reverseMap: make(map[string]string),
		pools:      make(map[string]*pgxpool.Pool),
		// mapMutex 和 poolMutex 默认是零值可用
	}
}

// RegisterConnection 实现 Service 接口。
func (s *pgxService) RegisterConnection(ctx context.Context, connString string) (string, error) {
	// 规范化连接字符串 (例如，确保有 postgresql:// 前缀)
	normalizedConnString, err := normalizeConnectionString(connString)
	if err != nil {
		return "", fmt.Errorf("无效的连接字符串格式: %w", err)
	}

	// --- 读锁保护检查是否存在 ---
	s.mapMutex.RLock()
	existingConnID, ok := s.reverseMap[normalizedConnString]
	s.mapMutex.RUnlock()
	// --- 读锁结束 ---

	if ok {
		utils.DefaultLogger.Info("连接字符串已注册，返回现有 connID:", zap.String("connID", existingConnID))
		// 可选：尝试 Ping 一下现有连接池确保可用
		if pool, poolExists := s.pools[existingConnID]; poolExists {
			go func() { // 异步 Ping，不阻塞注册流程
				pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := pool.Ping(pingCtx); err != nil {
					utils.DefaultLogger.Error("警告: 现有连接池Ping 失败:", zap.String("existingID", existingConnID), zap.Error(err))
					// 可以考虑在这里触发移除并强制重新创建池的逻辑，但这会增加复杂性
				}
			}()
		}
		return existingConnID, nil
	}

	// --- 写锁保护创建新映射 ---
	s.mapMutex.Lock()
	defer s.mapMutex.Unlock()

	// 双重检查，防止在获取写锁期间其他 goroutine 已经注册
	if existingConnID, ok = s.reverseMap[normalizedConnString]; ok {
		utils.DefaultLogger.Info("连接字符串在获取写锁期间已被注册，返回现有 connID:\n", zap.String("connID", existingConnID))
		return existingConnID, nil
	}

	// 生成新的唯一 connID
	newConnID, err := utils.ConnectionStringToUUID(normalizedConnString) // 假设 utils 包有此函数
	if err != nil {
		return "err", fmt.Errorf("无效的连接字符串格式: %w", err)
	}
	// 存储映射关系
	s.connMap[newConnID] = normalizedConnString
	s.reverseMap[normalizedConnString] = newConnID
	utils.DefaultLogger.Info("注册新连接:", zap.String("connID", newConnID), zap.String("connstring:", normalizedConnString[:20])) // 日志中隐藏部分连接串

	return newConnID, nil
}

// DisconnectConnection 实现 Service 接口。
func (s *pgxService) DisconnectConnection(ctx context.Context, connID string) error {
	s.mapMutex.Lock() // 获取写锁，因为要修改映射
	connString, ok := s.connMap[connID]
	if ok {
		delete(s.connMap, connID)
		delete(s.reverseMap, connString) // 清理反向映射
	}
	s.mapMutex.Unlock() // 释放映射锁

	if !ok {
		utils.DefaultLogger.Error("警告: 尝试断开未注册的:", zap.String("connID", connID))
		return errors.New("未知的 connID") // 或者返回 nil 允许幂等操作？根据需求决定
	}

	utils.DefaultLogger.Info("正在断开连接:", zap.String("connID", connID))

	// --- 锁保护关闭和删除 Pool ---
	s.poolMutex.Lock()
	defer s.poolMutex.Unlock()

	pool, poolExists := s.pools[connID]
	if poolExists {
		utils.DefaultLogger.Info("正在关闭连接池:", zap.String("connID", connID))
		pool.Close() // pgxpool.Close() 是同步的
		delete(s.pools, connID)
		utils.DefaultLogger.Info("连接池已关闭并移除:", zap.String("connID", connID))
	} else {
		utils.DefaultLogger.Warn("警告: 连接池不存在或已被关闭",
			zap.String("connID", connID))
	}
	// --- Pool 锁结束 ---

	return nil
}

// GetPool 实现 Service 接口。
func (s *pgxService) GetPool(ctx context.Context, connID string) (*pgxpool.Pool, error) {
	// --- 读锁保护获取连接字符串 ---
	s.mapMutex.RLock()
	connString, ok := s.connMap[connID]
	s.mapMutex.RUnlock()
	// --- 读锁结束 ---

	if !ok {
		return nil, fmt.Errorf("未知的 connID: %s", connID)
	}

	// --- 再次读锁检查 Pool 是否已存在 (避免获取更重的 poolMutex) ---
	s.mapMutex.RLock() // 这里用 mapMutex 的读锁保护 pools 的读操作
	pool, exists := s.pools[connID]
	s.mapMutex.RUnlock()
	// --- 读锁结束 ---

	if exists {
		return pool, nil // Pool 已存在，直接返回
	}

	// --- Pool 不存在，需要创建，获取 poolMutex ---
	s.poolMutex.Lock()
	defer s.poolMutex.Unlock()

	// --- 双重检查，防止在等待 poolMutex 期间 Pool 已被其他 goroutine 创建 ---
	s.mapMutex.RLock() // 再次读锁检查
	pool, exists = s.pools[connID]
	s.mapMutex.RUnlock()
	if exists {
		return pool, nil // 其他 goroutine 刚刚创建了它
	}
	// --- 结束双重检查 ---

	// --- 确认需要创建 Pool ---
	utils.DefaultLogger.Info("连接池不存在，为创建新连接池...",
		zap.String("connID", connID),
	)
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("解析连接字符串失败 (connID: %s): %w", connID, err)
	}

	// 应用配置中的连接池设置
	poolConfig.MaxConns = int32(s.config.DBMaxOpenConns)
	poolConfig.MinConns = int32(s.config.DBMinOpenConns)
	poolConfig.MaxConnLifetime = s.config.DBConnMaxLifetime
	poolConfig.MaxConnIdleTime = s.config.DBConnMaxIdleTime

	// 创建连接池
	// 使用 context.Background() 创建，因为池的生命周期与应用相关，不应被单个请求取消
	newPool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败 (connID: %s): %w", connID, err)
	}
	utils.DefaultLogger.Info("连接池创建成功:", zap.String("connID", connID))

	// --- 写锁保护添加新 Pool 到映射 ---
	s.mapMutex.Lock() // 需要写锁来修改 pools map
	s.pools[connID] = newPool
	s.mapMutex.Unlock()
	// --- 写锁结束 ---

	return newPool, nil
}

// ExecuteQuery 实现 Service 接口，委托给 executor。
func (s *pgxService) ExecuteQuery(ctx context.Context, connID string, readOnly bool, sql string, args ...any) ([]map[string]any, error) {
	pool, err := s.GetPool(ctx, connID)
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败 (connID: %s): %w", connID, err)
	}
	// 调用 executor.go 中的内部执行函数
	return executeQueryInternal(ctx, pool, readOnly, sql, args...)
}

// ExecuteNonQuery 实现 Service 接口，委托给 executor。
func (s *pgxService) ExecuteNonQuery(ctx context.Context, connID string, readOnly bool, sql string, args ...any) error {
	pool, err := s.GetPool(ctx, connID)
	if err != nil {
		return fmt.Errorf("获取连接池失败 (connID: %s): %w", connID, err)
	}
	// 调用 executor.go 中的内部执行函数
	return executeNonQueryInternal(ctx, pool, readOnly, sql, args...)
}

// CloseAll 实现 Service 接口。
func (s *pgxService) CloseAll(ctx context.Context) error {
	utils.DefaultLogger.Info("关闭所有连接池...")
	var MError error // 用于收集关闭过程中的错误

	s.poolMutex.Lock() // 锁住 pool map 进行迭代和删除
	s.mapMutex.Lock()  // 同时锁住 map，因为要清空
	defer s.poolMutex.Unlock()
	defer s.mapMutex.Unlock()

	for connID, pool := range s.pools {
		utils.DefaultLogger.Info("关闭连接池:", zap.String("connID", connID))
		pool.Close() // 同步关闭
	}

	// 清空所有映射
	s.pools = make(map[string]*pgxpool.Pool)
	s.connMap = make(map[string]string)
	s.reverseMap = make(map[string]string)
	utils.DefaultLogger.Info("所有数据库连接池已关闭。")
	return MError // 返回收集到的错误（如果需要更精细的错误处理）
}

// --- 内部辅助函数 ---

// normalizeConnectionString 确保连接字符串以 "postgresql://" 开头
func normalizeConnectionString(connString string) (string, error) {
	trimmed := strings.TrimSpace(connString)
	if trimmed == "" {
		return "", errors.New("连接字符串不能为空")
	}
	if !strings.HasPrefix(trimmed, "postgresql://") && !strings.HasPrefix(trimmed, "postgres://") {
		// 尝试解析以验证基本格式
		_, err := url.Parse("postgresql://" + trimmed) // 只是为了验证，不使用结果
		if err != nil {
			// 如果添加协议后仍然解析失败，说明原始格式可能有问题
			// 尝试直接解析原始字符串
			_, errDirect := url.Parse(trimmed)
			if errDirect != nil {
				return "", fmt.Errorf("连接字符串格式无效: %w", errDirect) // 报告直接解析的错误
			}
			// 如果直接解析成功，可能本身就是带协议的，虽然不是 postgresql://
			utils.DefaultLogger.Warn("警告: 连接字符串未使用 'postgresql://'",
				zap.String("前缀:", trimmed),
			)
			return trimmed, nil // 返回原始格式
		}
		// 添加协议后解析成功
		return "postgresql://" + trimmed, nil
	}
	// 本身就带有协议前缀，直接验证
	_, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("连接字符串格式无效: %w", err)
	}
	return trimmed, nil
}
