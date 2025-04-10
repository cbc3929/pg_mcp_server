# <center>PG_MCP_SERVER</center>

<center>自己实现的对于 PostGresQL 的支持</center>

## 介绍

通用的 Postgres 的 MCP Server Mcp 部分使用了 MCP-GO 来实现 支持 Stdio 和 SSE 传输。

> Postgis 和 PgVector 的描述来自另一个开源项目：https://github.com/stuzero/pg-mcp-server 🙏🙏🙏  
>  这种提示方式令人耳目一新

> ⚠️ 数据库需要定义角色来防止 SQL 注入 给 schema➡️public Selete 权限防止敏感数据注入  
> ⚠️ 新建的角色给 schema➡️temp 所有权限来保证数据隔离

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

## 插件支持

- PostGis ✅
- PgVector ✅
- PgRouting ⭕

## 特点

LLM 本地部署的情况需要合理分配上下文 如果每次调用都读取库增加时间也占用大量 Token，该项目采取的是预处理的方法 本身支持从库中获取表结构 并且以描述的方式来告诉 LLM：  
 利用 Tool 的 description 和 input_schema 来隐式或显式地传递 Schema 信息。

## 未完成

- 对于 Temp 架构下的表的梳理 应该有 监测机制来 对表进行回收
- 单元测试问题
