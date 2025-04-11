# <center>PG_MCP_SERVER</center>

<center>自己实现的对于 PostGresQL 的支持</center>

## 介绍

通用的 Postgres 的 MCP Server Mcp 部分使用了 MCP-GO 来实现 支持 Stdio 和 SSE 传输。

> Postgis 和 PgVector 的描述来自另一个开源项目：https://github.com/stuzero/pg-mcp-server 🙏🙏🙏  
>  这种提示方式令人耳目一新

> ⚠️ 数据库需要定义角色来防止 SQL 注入 给 schema➡️public Selete 权限防止敏感数据注入  
> ⚠️ 新建的角色给 schema➡️temp 所有权限来保证数据隔离

## 使用

在 `main.go` 中

```go
schemaLoadConnID, err := dbService.RegisterConnection(tempCtx, "postgres://mcp_user:mcp123456@192.168.2.19:5432/postgres")
```

这里可以设置和数据库的交互当然也可以更改为.env 中设置 只需打开`.env` 注释 `SCHEMA_LOAD_DB_URL`

这里的给服务器一个初始的连接`string`来缓存数据库表的信息  
这里推荐新建一个服务器角色 `sql`如下：

```sql
-- 新建一个角色 设置密码
CREATE ROLE mcp_user WITH LOGIN PASSWORD 'mcp123456';
-- 设置mcp_server的基本权限
GRANT CONNECT ON DATABASE postgres TO mcp_user;
GRANT USAGE ON SCHEMA public TO mcp_user;
-- 设置 public 架构下所有表的selete权限
GRANT SELECT ON ALL TABLES IN SCHEMA public TO mcp_user;
-- 使未来在 public schema 中创建的表的 SELECT 权限自动授予 mcp_user
ALTER DEFAULT PRIVILEGES IN SCHEMA public
   GRANT SELECT ON TABLES TO mcp_user;
-- 新建一个 temp 的 schema
CREATE SCHEMA temp;
-- 给mcp_user用户 所有的 temp 架构下的权限
GRANT USAGE, CREATE ON SCHEMA temp TO mcp_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA temp TO mcp_user;
-- 同样的 未来所有的 schema 的权限都赋予给mcp_user
ALTER DEFAULT PRIVILEGES IN SCHEMA temp
   GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO mcp_user;
```

## 运行

- 🐋Docker 运行

```shell
git clone https://github.com/cbc3929/pg_mcp_server.git
cd pg_mcp_server
docker build -t pg-mcp-server:latest .
docker run -d -p 8181:8181 --name my-mcp-server pg-mcp-server:latest
```

- 🀄 直接运行

1. 克隆项目

```shell
git clone https://github.com/cbc3929/pg_mcp_server.git
cd pg_mcp_server
```

2. 安装依赖

```bash
go mod tidy
```

或者

```bash
go mod download
```

3. 直接运行

```bash
go run main.go
```

4. 打包(可选)

```bash
go build
./pg_mcp_server
```

## 插件支持

- PostGis ✅
- PgVector ✅
- PgRouting ⭕

## 特点

LLM 本地部署的情况需要合理分配上下文 如果每次调用都读取库增加时间也占用大量 Token，该项目采取的是预处理的方法 本身支持从库中获取表结构 并且以描述的方式来告诉 LLM：
利用 Tool 的 description 和 input_schema 来隐式或显式地传递 Schema 信息。
利用 MCP 中的 Resource 在初始化的时候就读取了 表包含名字 列 约束 外键 索引 Geom 的类型和 EPSG 为大模型深入理解创造了基本的条件

## 未完成

- 对于 Temp 架构下的表的梳理 应该有 监测机制来 对表进行回收
- 单元测试问题

```

```
