package schemas

import (
	"context" // 用于处理可能的 NULL 字符串
	"fmt"
	"sync"

	// 引入数据库服务接口
	"github.com/cbc3929/pg_mcp_server/internal/core/databases"
	"github.com/cbc3929/pg_mcp_server/internal/utils" // 引入日志

	"go.uber.org/zap" // 引入 zap 日志
)

// Manager 定义了 Schema 管理器的接口
type Manager interface {
	// LoadSchema 从数据库加载 Schema 信息并缓存。
	// connID: 用于执行 Schema 查询的数据库连接 ID。
	LoadSchema(ctx context.Context, connID string) error

	// GetDatabaseInfo 返回缓存的整个数据库结构信息。
	GetDatabaseInfo() (*DatabaseInfo, bool)

	// GetSchemaInfo 返回指定名称的 Schema 的缓存信息。
	GetSchemaInfo(schemaName string) (*SchemaInfo, bool)

	// GetTableInfo 返回指定 Schema 和表名的表的缓存信息。
	GetTableInfo(schemaName, tableName string) (*TableInfo, bool)
}

// manager 是 SchemaManager 接口的实现。
type manager struct {
	dbService databases.Service // 数据库服务依赖
	cache     *DatabaseInfo     // 内存缓存
	mu        sync.RWMutex      // 保护缓存的读写锁
}

// NewManager 创建一个新的 Schema Manager 实例。
// dbService: 数据库服务实例，用于执行查询。
func NewManager(dbService databases.Service) Manager {
	utils.DefaultLogger.Info("初始化 Schema 管理器...")
	return &manager{
		dbService: dbService,
		cache:     &DatabaseInfo{Schemas: []SchemaInfo{}}, // 初始化空缓存
		// mu 默认零值可用
	}
}

// LoadSchema 实现 Manager 接口。
func (m *manager) LoadSchema(ctx context.Context, connID string) error {
	utils.DefaultLogger.Info("开始加载数据库 Schema 信息...", zap.String("connID", connID))

	m.mu.Lock() // 获取写锁以更新缓存
	defer m.mu.Unlock()

	newCache := &DatabaseInfo{Schemas: []SchemaInfo{}}

	// 1. 获取所有相关的 Schema
	schemas, err := m.fetchSchemas(ctx, connID)
	if err != nil {
		utils.DefaultLogger.Error("获取 Schema 列表失败", zap.String("connID", connID), zap.Error(err))
		return fmt.Errorf("获取 Schema 列表失败: %w", err)
	}
	if len(schemas) == 0 {
		utils.DefaultLogger.Warn("未在数据库中找到用户相关的 Schema", zap.String("connID", connID))
		m.cache = newCache // 更新为空缓存
		return nil         // 没有 Schema 就无需继续
	}
	utils.DefaultLogger.Info("成功获取 Schema 列表", zap.Int("count", len(schemas)), zap.String("connID", connID))

	newCache.Schemas = make([]SchemaInfo, 0, len(schemas))
	for _, s := range schemas {
		schemaInfo := SchemaInfo{
			Name:        s["schema_name"].(string),
			Description: dbString(s["description"]), // 处理可能的 NULL
			Tables:      []TableInfo{},
		}

		// 2. 获取当前 Schema 下的所有表
		tables, err := m.fetchTables(ctx, connID, schemaInfo.Name)
		if err != nil {
			utils.DefaultLogger.Error("获取表信息失败", zap.String("schema", schemaInfo.Name), zap.String("connID", connID), zap.Error(err))
			// 选择继续处理其他 Schema 还是直接返回错误？这里选择继续
			continue
		}
		schemaInfo.Tables = make([]TableInfo, 0, len(tables))

		// 3. 获取每个表的详细信息 (列, 索引, 外键)
		for _, t := range tables {
			tableName := t["table_name"].(string)
			tableInfo := TableInfo{
				Name:        tableName,
				Description: dbString(t["description"]),
				RowCount:    dbInt64(t["row_count"]), // 大致行数
				Columns:     []ColumnInfo{},
				Indexes:     []IndexInfo{},
				ForeignKeys: []ForeignKeyInfo{},
			}

			// 3a. 获取列信息
			columns, err := m.fetchColumns(ctx, connID, schemaInfo.Name, tableName)
			if err != nil {
				utils.DefaultLogger.Error("获取列信息失败", zap.String("schema", schemaInfo.Name), zap.String("table", tableName), zap.String("connID", connID), zap.Error(err))
				continue // 继续处理下一张表
			}
			tableInfo.Columns = columns // columns 已经在 fetchColumns 中组装好

			// 3b. 获取索引信息
			indexes, err := m.fetchIndexes(ctx, connID, schemaInfo.Name, tableName)
			if err != nil {
				utils.DefaultLogger.Error("获取索引信息失败", zap.String("schema", schemaInfo.Name), zap.String("table", tableName), zap.String("connID", connID), zap.Error(err))
				// 索引信息通常不是最关键的，选择继续
			} else {
				tableInfo.Indexes = indexes
			}

			// 3c. 获取外键信息
			foreignKeys, err := m.fetchForeignKeys(ctx, connID, schemaInfo.Name, tableName)
			if err != nil {
				utils.DefaultLogger.Error("获取外键信息失败", zap.String("schema", schemaInfo.Name), zap.String("table", tableName), zap.String("connID", connID), zap.Error(err))
				// 外键信息比较重要，但也可以选择继续
			} else {
				tableInfo.ForeignKeys = foreignKeys
			}

			schemaInfo.Tables = append(schemaInfo.Tables, tableInfo)
		}
		newCache.Schemas = append(newCache.Schemas, schemaInfo)
	}

	m.cache = newCache // 原子地替换整个缓存
	utils.DefaultLogger.Info("数据库 Schema 信息加载并缓存完成", zap.String("connID", connID))
	return nil
}

// GetDatabaseInfo 实现 Manager 接口。
func (m *manager) GetDatabaseInfo() (*DatabaseInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cache == nil || len(m.cache.Schemas) == 0 {
		return nil, false
	}
	// 返回缓存的深拷贝还是浅拷贝？取决于使用场景。这里返回指针（浅拷贝）。
	// 如果需要防止外部修改缓存，应考虑返回深拷贝。
	return m.cache, true
}

// GetSchemaInfo 实现 Manager 接口。
func (m *manager) GetSchemaInfo(schemaName string) (*SchemaInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cache == nil {
		return nil, false
	}
	for i := range m.cache.Schemas {
		if m.cache.Schemas[i].Name == schemaName {
			return &m.cache.Schemas[i], true // 返回找到的 SchemaInfo 指针
		}
	}
	return nil, false // 未找到
}

// GetTableInfo 实现 Manager 接口。
func (m *manager) GetTableInfo(schemaName, tableName string) (*TableInfo, bool) {
	schemaInfo, found := m.GetSchemaInfo(schemaName) // 利用已有方法
	if !found {
		return nil, false
	}
	// 不需要再次加锁，因为 GetSchemaInfo 内部已经处理了锁
	for i := range schemaInfo.Tables {
		if schemaInfo.Tables[i].Name == tableName {
			return &schemaInfo.Tables[i], true // 返回找到的 TableInfo 指针
		}
	}
	return nil, false // 未找到
}

// --- 内部查询辅助函数 ---

func (m *manager) fetchSchemas(ctx context.Context, connID string) ([]map[string]any, error) {
	query := `
        SELECT
            schema_name,
            obj_description(pg_namespace.oid, 'pg_namespace') as description -- 使用正确的 obj_description 用法
        FROM information_schema.schemata
        JOIN pg_namespace ON pg_namespace.nspname = schema_name
        WHERE
            schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
            AND schema_name NOT LIKE 'pg\_%' ESCAPE '\' -- 正确转义下划线
            AND schema_name NOT LIKE 'temp%' -- 排除我们自己的临时 schema
						AND schema_name NOT LIKE 'topolo%' -- 排除postgis的 schema
        ORDER BY schema_name
    `
	return m.dbService.ExecuteQuery(ctx, connID, true, query) // 只读查询
}

func (m *manager) fetchTables(ctx context.Context, connID, schemaName string) ([]map[string]any, error) {
	query := `
        SELECT
            t.table_name,
            obj_description(c.oid, 'pg_class') as description, -- 使用 pg_class oid
            c.reltuples::bigint as row_count -- 使用 pg_class.reltuples 获取大致行数
        FROM information_schema.tables t
        JOIN pg_namespace n ON t.table_schema = n.nspname
        JOIN pg_class c ON t.table_name = c.relname AND n.oid = c.relnamespace
        WHERE
            t.table_schema = $1
            AND t.table_type = 'BASE TABLE'
            AND c.relkind = 'r' -- 确保是普通表 ('r')
						AND t.table_name NOT LIKE 'spatia%' -- Postgis 的空间坐标系的表排除
        ORDER BY t.table_name
    `
	return m.dbService.ExecuteQuery(ctx, connID, true, query, schemaName)
}

func (m *manager) fetchColumns(ctx context.Context, connID, schemaName, tableName string) ([]ColumnInfo, error) {
	// 获取基本列信息
	queryColumns := `
        SELECT
            c.column_name,
            format_type(a.atttypid, a.atttypmod) AS formatted_type, -- 获取完整格式化类型
            c.is_nullable,
            c.column_default,
            col_description(cls.oid, c.ordinal_position) as description
            -- 也可以选择性地保留 c.data_type, c.udt_name 用于调试
            -- c.data_type,
            -- c.udt_name
        FROM information_schema.columns c -- 主要用于获取列顺序和基本信息
        JOIN pg_namespace ns ON c.table_schema = ns.nspname
        JOIN pg_class cls ON c.table_name = cls.relname AND ns.oid = cls.relnamespace
        JOIN pg_attribute a ON a.attrelid = cls.oid AND a.attname = c.column_name -- 关联 pg_attribute 获取类型 OID 和修饰符
        WHERE
            c.table_schema = $1 AND
            c.table_name = $2
            AND cls.relkind = 'r'
            AND a.attnum > 0 -- 排除系统列
            AND NOT a.attisdropped -- 排除已删除的列
        ORDER BY c.ordinal_position -- 保持 information_schema 的顺序
    `
	rows, err := m.dbService.ExecuteQuery(ctx, connID, true, queryColumns, schemaName, tableName)
	if err != nil {
		return nil, err
	}

	// 获取表的所有约束信息，以便后面匹配
	constraints, err := m.fetchConstraintsForTable(ctx, connID, schemaName, tableName)
	if err != nil {
		utils.DefaultLogger.Warn("获取约束信息失败，列信息中将缺少约束详情",
			zap.String("schema", schemaName), zap.String("table", tableName), zap.Error(err))
		constraints = nil // 置空，后续逻辑会处理 nil
	}

	columns := make([]ColumnInfo, 0, len(rows))
	for _, row := range rows {
		colName := row["column_name"].(string)
		col := ColumnInfo{
			Name:         colName,
			Type:         dbString(row["formatted_type"]),
			IsNullable:   row["is_nullable"].(string) == "YES",
			DefaultValue: dbStringPtr(row["column_default"]), // 处理可能的 NULL 默认值
			Description:  dbString(row["description"]),
			Constraints:  []ColumnConstraint{}, // 初始化为空切片
		}

		// 匹配约束
		if constraints != nil {
			for _, constr := range constraints {
				constrCols, _ := constr["column_names"].([]string) // 需要类型断言和检查
				constrTypeDesc, _ := constr["constraint_type_desc"].(string)
				if stringInSlice(colName, constrCols) {
					// 只添加非外键和非NotNull的约束类型到列上（外键单独处理，NotNull由IsNullable表示）
					cc := ColumnConstraint(constrTypeDesc)
					if cc != ForeignKeyConstraint && constrTypeDesc != "" {
						col.Constraints = append(col.Constraints, cc)
					}
				}
			}
		}

		columns = append(columns, col)
	}

	return columns, nil
}

func (m *manager) fetchIndexes(ctx context.Context, connID, schemaName, tableName string) ([]IndexInfo, error) {
	query := `
        SELECT
						i.relname as index_name,
						am.amname as index_type,
						ix.indisunique as is_unique,
						ix.indisprimary as is_primary,
						obj_description(i.oid, 'pg_class') as description,
						pg_get_indexdef(i.oid) as index_definition, -- 获取完整定义
						-- 获取索引列名，处理表达式索引
						array_agg(
								CASE
										WHEN ix.indkey[k.attpos] > 0 THEN a.attname -- 普通列 (使用 ix.indkey[k.attpos] 获取 attnum)
										ELSE pg_get_indexdef(i.oid, k.i::int, false) -- 表达式 (k.i 是行号/列位置)
								END
								ORDER BY k.i -- 按索引中的列顺序排序
						) as column_names
				FROM
						pg_index ix
				JOIN
						pg_class i ON i.oid = ix.indexrelid
				JOIN
						pg_class t ON t.oid = ix.indrelid
				JOIN
						pg_namespace n ON n.oid = t.relnamespace
				JOIN
						pg_am am ON i.relam = am.oid
				LEFT JOIN
						-- k.attpos 是 indkey 的下标 (从 1 开始), k.i 是 generate_subscripts 的行号 (也是从 1 开始)
						generate_subscripts(ix.indkey, 1) WITH ORDINALITY AS k(attpos, i) ON TRUE
				LEFT JOIN
						-- 使用正确的 attnum (ix.indkey[k.attpos]) 来关联 pg_attribute
						pg_attribute a ON a.attrelid = t.oid AND a.attnum = ix.indkey[k.attpos] -- 使用 ix.indkey[k.attpos]
				WHERE
						n.nspname = $1
						AND t.relname = $2
						AND ix.indislive -- 只选择有效的索引
				GROUP BY
						i.relname, i.oid, am.amname, ix.indisunique, ix.indisprimary
				ORDER BY
						i.relname;
    `
	rows, err := m.dbService.ExecuteQuery(ctx, connID, true, query, schemaName, tableName)
	if err != nil {
		return nil, err
	}

	indexes := make([]IndexInfo, 0, len(rows))
	for _, row := range rows {
		// 需要小心处理 array_agg 返回的类型，它可能是 []interface{} 或特定类型数组
		var cols []string
		if colsInterface, ok := row["column_names"].([]interface{}); ok {
			cols = make([]string, len(colsInterface))
			for i, v := range colsInterface {
				if str, ok := v.(string); ok {
					cols[i] = str
				}
			}
		} else if colsString, ok := row["column_names"].([]string); ok { // 可能直接是 []string
			cols = colsString
		}

		idx := IndexInfo{
			IndexName:       row["index_name"].(string),
			IndexType:       row["index_type"].(string),
			Columns:         cols,
			IsUnique:        row["is_unique"].(bool),
			IsPrimary:       row["is_primary"].(bool),
			IndexDefinition: dbString(row["index_definition"]),
			Description:     dbString(row["description"]),
		}
		indexes = append(indexes, idx)
	}
	return indexes, nil
}

func (m *manager) fetchForeignKeys(ctx context.Context, connID, schemaName, tableName string) ([]ForeignKeyInfo, error) {
	query := `
        SELECT
            c.conname as constraint_name,
            ARRAY_AGG(col.attname ORDER BY u.attposition) as column_names,
            nr.nspname as referenced_schema,
            ref_table.relname as referenced_table,
            ARRAY_AGG(ref_col.attname ORDER BY u2.attposition) as referenced_columns,
            obj_description(c.oid, 'pg_constraint') as description
        FROM
            pg_constraint c
        JOIN
            pg_namespace n ON n.oid = c.connamespace
        JOIN
            pg_class t ON t.oid = c.conrelid
        JOIN
            pg_class ref_table ON ref_table.oid = c.confrelid
        JOIN
            pg_namespace nr ON nr.oid = ref_table.relnamespace
        LEFT JOIN
            LATERAL unnest(c.conkey) WITH ORDINALITY AS u(attnum, attposition) ON TRUE
        LEFT JOIN
            pg_attribute col ON col.attrelid = t.oid AND col.attnum = u.attnum
        LEFT JOIN
            LATERAL unnest(c.confkey) WITH ORDINALITY AS u2(attnum, attposition) ON TRUE
        LEFT JOIN
            pg_attribute ref_col ON ref_col.attrelid = c.confrelid AND ref_col.attnum = u2.attnum
        WHERE
            n.nspname = $1
            AND t.relname = $2
            AND c.contype = 'f' -- 只选择外键约束
        GROUP BY
            c.conname, nr.nspname, ref_table.relname, c.oid
        ORDER BY
            c.conname
    `
	rows, err := m.dbService.ExecuteQuery(ctx, connID, true, query, schemaName, tableName)
	if err != nil {
		return nil, err
	}

	foreignKeys := make([]ForeignKeyInfo, 0, len(rows))
	for _, row := range rows {
		// 处理可能的数组类型转换
		cols := interfaceSliceToStringSlice(row["column_names"])
		refCols := interfaceSliceToStringSlice(row["referenced_columns"])

		fk := ForeignKeyInfo{
			ConstraintName:    row["constraint_name"].(string),
			Columns:           cols,
			ReferencedSchema:  row["referenced_schema"].(string),
			ReferencedTable:   row["referenced_table"].(string),
			ReferencedColumns: refCols,
			Description:       dbString(row["description"]),
		}
		foreignKeys = append(foreignKeys, fk)
	}
	return foreignKeys, nil
}

// fetchConstraintsForTable 获取指定表的所有约束信息 (供内部使用)
func (m *manager) fetchConstraintsForTable(ctx context.Context, connID, schemaName, tableName string) ([]map[string]any, error) {
	query := `
        SELECT
            c.conname as constraint_name,
            c.contype as constraint_type,
            CASE
                WHEN c.contype = 'p' THEN 'PRIMARY KEY'
                WHEN c.contype = 'u' THEN 'UNIQUE'
                WHEN c.contype = 'f' THEN 'FOREIGN KEY'
                WHEN c.contype = 'c' THEN 'CHECK'
                ELSE 'OTHER'
            END as constraint_type_desc,
            ARRAY_AGG(col.attname ORDER BY u.attposition) filter (where col.attname is not null) as column_names -- 过滤掉可能的 NULL
        FROM
            pg_constraint c
        JOIN
            pg_namespace n ON n.oid = c.connamespace
        JOIN
            pg_class t ON t.oid = c.conrelid
        LEFT JOIN
            LATERAL unnest(c.conkey) WITH ORDINALITY AS u(attnum, attposition) ON TRUE
        LEFT JOIN
            pg_attribute col ON col.attrelid = t.oid AND col.attnum = u.attnum
        WHERE
            n.nspname = $1
            AND t.relname = $2
        GROUP BY
            c.conname, c.contype
        ORDER BY
            c.contype, c.conname
    `
	return m.dbService.ExecuteQuery(ctx, connID, true, query, schemaName, tableName)
}

// --- 数据库 NULL 值处理辅助函数 ---

// dbString 安全地从 map[string]any 中获取字符串，处理 nil
func dbString(v any) string {
	if v == nil {
		return ""
	}
	if str, ok := v.(string); ok {
		return str
	}
	// 可以选择记录一个警告或返回空字符串
	utils.DefaultLogger.Warn("预期数据库返回字符串，但类型不匹配", zap.Any("value", v))
	return ""
}

// dbStringPtr 安全地从 map[string]any 中获取字符串指针，处理 nil
func dbStringPtr(v any) *string {
	if v == nil {
		return nil
	}
	if str, ok := v.(string); ok {
		return &str
	}
	utils.DefaultLogger.Warn("预期数据库返回字符串（用于指针），但类型不匹配", zap.Any("value", v))
	return nil
}

// dbInt64 安全地从 map[string]any 中获取 int64，处理 nil 和类型转换
func dbInt64(v any) int64 {
	if v == nil {
		return 0
	}
	// pgx 可能返回 int, int32, int64 等，尝试转换
	switch val := v.(type) {
	case int64:
		return val
	case int32:
		return int64(val)
	case int:
		return int64(val)
	// 可以添加 float64 等其他类型的处理
	case float64: // reltuples 返回 float8
		return int64(val)
	case float32:
		return int64(val)
	default:
		utils.DefaultLogger.Warn("预期数据库返回整数类型，但类型不匹配", zap.Any("value", v), zap.String("type", fmt.Sprintf("%T", v)))
		return 0
	}
}

// stringInSlice 检查字符串是否在字符串切片中
func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// interfaceSliceToStringSlice 将 []any (通常来自数据库驱动) 转换为 []string
func interfaceSliceToStringSlice(slice any) []string {
	if slice == nil {
		return nil
	}
	if interfaceSlice, ok := slice.([]any); ok {
		stringSlice := make([]string, 0, len(interfaceSlice))
		for _, item := range interfaceSlice {
			if item == nil { // 跳过 nil 元素
				continue
			}
			if str, ok := item.(string); ok {
				stringSlice = append(stringSlice, str)
			} else {
				// 可以记录警告
				utils.DefaultLogger.Warn("数组元素类型不是字符串", zap.Any("value", item))
			}
		}
		// 如果原始数组为空但非nil，返回空切片而不是 nil
		if len(stringSlice) == 0 && len(interfaceSlice) > 0 {
			return []string{}
		}
		return stringSlice
	} else if stringSlice, ok := slice.([]string); ok { // 可能已经是 []string
		return stringSlice
	}

	utils.DefaultLogger.Warn("预期数据库返回数组类型，但类型不匹配", zap.Any("value", slice), zap.String("type", fmt.Sprintf("%T", slice)))
	return nil // 或者返回 []string{} ?
}
