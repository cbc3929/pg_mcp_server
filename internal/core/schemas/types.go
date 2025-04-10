package schemas

// 约束
type ColumnConstraint string

// 约束的种类
const (
	PrimaryKeyConstraint ColumnConstraint = "PRIMARY KEY"
	ForeignKeyConstraint ColumnConstraint = "FOREIGN KEY"
	UniqueConstraint     ColumnConstraint = "UNIQUE"
	CheckConstraint      ColumnConstraint = "CHECK"
	NotNullConstraint    ColumnConstraint = "NOT NULL" // Note: Usually handled by IsNullable field
)

// 主键的关键字
type ForeignKeyInfo struct {
	ConstraintName    string   `json:"name" yaml:"name"`                                   // 约束名称
	Columns           []string `json:"columns" yaml:"columns"`                             // 此表中参与外键的列
	ReferencedSchema  string   `json:"referenced_schema" yaml:"referenced_schema"`         // 引用的 Schema
	ReferencedTable   string   `json:"referenced_table" yaml:"referenced_table"`           // 引用的表
	ReferencedColumns []string `json:"referenced_columns" yaml:"referenced_columns"`       // 引用的列
	Description       string   `json:"description,omitempty" yaml:"description,omitempty"` // (可选) 约束的注释
}

// 索引的关键字
type IndexInfo struct {
	IndexName       string   `json:"name" yaml:"name"`                                   // 索引名称
	IndexType       string   `json:"type" yaml:"type"`                                   // 索引类型 (e.g., btree, hash, gist, gin)
	Columns         []string `json:"columns" yaml:"columns"`                             // 索引包含的列名
	IsUnique        bool     `json:"is_unique" yaml:"is_unique"`                         // 是否唯一索引
	IsPrimary       bool     `json:"is_primary" yaml:"is_primary"`                       // 是否主键索引 (通常与主键约束关联)
	IndexDefinition string   `json:"definition,omitempty" yaml:"definition,omitempty"`   // 索引的 SQL 定义 (可选)
	Description     string   `json:"description,omitempty" yaml:"description,omitempty"` // (可选) 索引的注释
}

// 列的信息
type ColumnInfo struct {
	Name         string             `json:"name" yaml:"name"`                                   // 列名
	Type         string             `json:"type" yaml:"type"`                                   // 数据类型 (e.g., integer, varchar, timestamp with time zone)
	IsNullable   bool               `json:"nullable" yaml:"nullable"`                           // 是否允许 NULL 值
	DefaultValue *string            `json:"default,omitempty" yaml:"default,omitempty"`         // 默认值 (注意: 可能为 NULL)
	Description  string             `json:"description,omitempty" yaml:"description,omitempty"` // 列注释
	Constraints  []ColumnConstraint `json:"constraints,omitempty" yaml:"constraints,omitempty"` // 应用于此列的约束类型 (非 NotNull)
}

// 表的信息
type TableInfo struct {
	Name        string           `json:"name" yaml:"name"`                                     // 表名
	Description string           `json:"description,omitempty" yaml:"description,omitempty"`   // 表注释
	RowCount    int64            `json:"row_count" yaml:"row_count"`                           // 大致行数
	Columns     []ColumnInfo     `json:"columns" yaml:"columns"`                               // 表的列信息
	Indexes     []IndexInfo      `json:"indexes,omitempty" yaml:"indexes,omitempty"`           // 表的索引信息 (可选加载)
	ForeignKeys []ForeignKeyInfo `json:"foreign_keys,omitempty" yaml:"foreign_keys,omitempty"` // 表的外键信息 (可选加载)
}

// 架构的信息
type SchemaInfo struct {
	Name        string      `json:"name" yaml:"name"`                                   // Schema 名称
	Description string      `json:"description,omitempty" yaml:"description,omitempty"` // Schema 注释
	Tables      []TableInfo `json:"tables" yaml:"tables"`                               // Schema 下的表信息
}

// 数据库下的架构的信息
type DatabaseInfo struct {
	Schemas []SchemaInfo `json:"schemas" yaml:"schemas"` // 数据库中的所有相关 Schema
}
