package databases

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbc3929/pg_mcp_server/internal/utils"
	"go.uber.org/zap"

	"github.com/jackc/pgx/v5"        // pgx 核心
	"github.com/jackc/pgx/v5/pgconn" // 用于错误类型检查
	"github.com/jackc/pgx/v5/pgxpool"
)

// executeQueryInternal 是实际执行 SQL 查询并返回结果的内部函数。
// 它处理事务和只读模式。
func executeQueryInternal(ctx context.Context, pool *pgxpool.Pool, readOnly bool, sql string, args ...any) ([]map[string]any, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	defer conn.Release() // 确保连接在使用后返回池中

	txOptions := pgx.TxOptions{}
	if readOnly {
		txOptions.AccessMode = pgx.ReadOnly
		utils.DefaultLogger.Info("数据库操作: 只读模式,", zap.String(" SQL:", sql))
	} else {
		// !! 警告: 读写模式 !!
		// !! 必须确保调用此函数的工具层已经验证过 SQL 目标仅限于 temp schema !!
		txOptions.AccessMode = pgx.ReadWrite
		utils.DefaultLogger.Warn("数据库操作: 读写模式,", zap.String(" SQL:", sql))
	}

	tx, err := conn.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, fmt.Errorf("开始数据库事务失败: %w", err)
	}
	// 确保事务最终会被处理 (回滚未提交的)
	defer func() {
		// 如果事务还未提交或回滚 (例如因为 panic 或 Commit 失败后的 return)，尝试回滚
		_ = tx.Rollback(ctx) // 忽略回滚错误
	}()

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		// 回滚事务
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			utils.DefaultLogger.Warn("警告: 查询错误后回滚事务失败:,", zap.Error(rollbackErr), zap.Error(err))
		}
		// 检查是否是 PostgreSQL 错误并提供更详细信息
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return nil, fmt.Errorf("数据库查询执行错误: %s (Code: %s, Detail: %s): %w", pgErr.Message, pgErr.Code, pgErr.Detail, err)
		}
		return nil, fmt.Errorf("数据库查询执行错误: %w", err)
	}
	defer rows.Close() // 确保 rows 被关闭

	// 将结果行转换为 map 切片
	results, err := rowsToMaps(rows)
	if err != nil {
		// 此时查询已成功，但处理结果失败，仍然需要回滚吗？通常不需要，但可以记录错误。
		// 这里选择不回滚，因为查询本身是成功的，只是数据转换出问题。
		utils.DefaultLogger.Error("警告: 转换查询结果失败,", zap.Error(err))
		// 可以选择返回部分成功的结果和错误，或者直接返回错误
		// return results, fmt.Errorf("转换查询结果失败: %w", err)
		// 或者返回空和错误
		return nil, fmt.Errorf("转换查询结果失败: %w", err)
	}

	// 显式检查 rows.Err()，确保迭代过程中没有错误
	if err := rows.Err(); err != nil {
		utils.DefaultLogger.Error("警告: 迭代查询结果时发生错误,", zap.Error(err))
		// 同上，可能不需要回滚，但需要报告错误
		return nil, fmt.Errorf("迭代查询结果时发生错误: %w", err)
	}

	// 提交事务
	if err := tx.Commit(ctx); err != nil {
		// 提交失败，事务状态未知，可能已部分完成或完全回滚
		// 此时结果 `results` 可能不完全可靠（虽然通常数据已读出）
		utils.DefaultLogger.Error("警告: 提交数据库事务失败", zap.Error(err))
		// 根据业务需求决定是否返回已读取的数据和错误，或者只返回错误
		return nil, fmt.Errorf("提交数据库事务失败: %w", err)
	}

	return results, nil
}

// executeNonQueryInternal 是实际执行不返回结果的 SQL 命令的内部函数。
func executeNonQueryInternal(ctx context.Context, pool *pgxpool.Pool, readOnly bool, sql string, args ...any) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	defer conn.Release()

	txOptions := pgx.TxOptions{}
	if readOnly {
		txOptions.AccessMode = pgx.ReadOnly
		utils.DefaultLogger.Info("数据库操作 (NonQuery): 只读模式,", zap.String(" SQL:", sql))
	} else {
		// !! 警告: 读写模式 !!
		txOptions.AccessMode = pgx.ReadWrite
		utils.DefaultLogger.Warn("数据库操作 (NonQuery): 读写模式! ", zap.String(" SQL:", sql))
	}

	tx, err := conn.BeginTx(ctx, txOptions)
	if err != nil {
		return fmt.Errorf("开始数据库事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) // 确保未提交的事务被回滚
	}()

	// 执行命令
	commandTag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			utils.DefaultLogger.Warn("警告: 查询错误后回滚事务失败:,", zap.Error(rollbackErr), zap.Error(err))
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return fmt.Errorf("数据库命令执行错误: %s (Code: %s, Detail: %s): %w", pgErr.Message, pgErr.Code, pgErr.Detail, err)
		}
		return fmt.Errorf("数据库命令执行错误: %w", err)
	}
	utils.DefaultLogger.Info("数据库命令执行成功", zap.String(" 命令:", commandTag.String()), zap.Int64(" 影响行数:", commandTag.RowsAffected()))

	// 提交事务
	if err := tx.Commit(ctx); err != nil {
		utils.DefaultLogger.Error("提交数据库事务失败,", zap.Error(err))
		return fmt.Errorf("提交数据库事务失败: %w", err)
	}

	return nil
}

// rowsToMaps 将 pgx.Rows 转换为 []map[string]any
func rowsToMaps(rows pgx.Rows) ([]map[string]any, error) {
	fieldDescriptions := rows.FieldDescriptions()
	var results []map[string]any

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("读取行数据失败: %w", err)
		}

		rowMap := make(map[string]any, len(fieldDescriptions))
		for i, fd := range fieldDescriptions {
			// 需要处理可能的 NULL 值或其他类型转换问题吗？
			// pgx 通常能处理好，但复杂类型可能需要特殊处理。
			rowMap[fd.Name] = values[i]
		}
		results = append(results, rowMap)
	}

	// 检查迭代过程中是否有错误
	if err := rows.Err(); err != nil {
		return results, fmt.Errorf("迭代结果行时出错: %w", err) // 可能返回部分结果和错误
	}

	return results, nil
}
