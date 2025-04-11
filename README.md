# PG_MCP_SERVER

<div align="center">Custom Implementation for PostgreSQL + PostGIS Support</div>

## Introduction

[中文文档](./MD/README_ZH.md)

A general-purpose PostgreSQL MCP Server. The MCP part is implemented using [go-mcp](https://github.com/ThinkInAIXYZ/go-mcp), supporting Stdio and SSE transports.

> The descriptions for PostGIS and PgVector come from another open-source project: https://github.com/stuzero/pg-mcp-server 🙏🙏🙏
> This approach to providing context is refreshing.

> ⚠️ Database requires defining roles to prevent SQL injection. Grant `SELECT` permission to the `public` schema to prevent sensitive data exposure.
> ⚠️ Grant all permissions to the new role for the `temp` schema to ensure data isolation.

## Features

When deploying LLMs locally, context needs to be managed efficiently. Reading the database schema on every call consumes time and significant token context. This project adopts a pre-processing approach. It natively supports fetching the table structure from the database and provides descriptive information to the LLM:

- Utilizes Tool descriptions and input schemas to implicitly or explicitly convey schema information.
- Leverages MCP Resources during initialization to read table names, columns, constraints, foreign keys, indexes, geometry types, and EPSG codes, creating a fundamental understanding for the large model.

## Installation

In `main.go`:

```go
// Here you can set the connection string for database interaction
schemaLoadConnID, err := dbService.RegisterConnection(tempCtx, "postgres://mcp_user:mcp123456@192.168.2.19:5432/postgres")
```

The format is `postgres://user:pass@host:port/db`  
Alternatively, you can configure it via .env by uncommenting and setting the `SCHEMA_LOAD_DB_URL` variable.
Here, RegisterConnection gives the server an initial connection string to cache database table information.  
🏁 It is recommended to create a dedicated server role. The SQL is as follows:

```sql
-- Create a new role and set a password
CREATE ROLE mcp_user WITH LOGIN PASSWORD 'mcp123456';

-- Set basic permissions for mcp_user
GRANT CONNECT ON DATABASE postgres TO mcp_user;
GRANT USAGE ON SCHEMA public TO mcp_user;

-- Grant SELECT permission on all existing tables in the public schema
GRANT SELECT ON ALL TABLES IN SCHEMA public TO mcp_user;

-- Automatically grant SELECT on future tables in public schema to mcp_user
ALTER DEFAULT PRIVILEGES IN SCHEMA public
   GRANT SELECT ON TABLES TO mcp_user;

-- Create a temp schema
CREATE SCHEMA temp;

-- Grant all privileges on the temp schema to mcp_user
GRANT USAGE, CREATE ON SCHEMA temp TO mcp_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA temp TO mcp_user;

-- Grant all privileges on future tables in temp schema to mcp_user
ALTER DEFAULT PRIVILEGES IN SCHEMA temp
   GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO mcp_user;
Use code with caution.
```

## Running

- 🐋 Run with Docker

```bash
git clone https://github.com/cbc3929/pg_mcp_server.git
cd pg_mcp_server
docker build -t pg-mcp-server:latest .
docker run -d -p 8181:8181 --name my-mcp-server pg-mcp-server:latest
```

- 🀄 Run Directly

1. Clone the project

```
git clone https://github.com/cbc3929/pg_mcp_server.git
cd pg_mcp_server
```

2. Install dependencies

```bash
go mod tidy
```

or

```bash
go mod download
```

3. Run directly  
   Ensure necessary environment variables are set or .env file is present

```
go run ./cmd/server/main.go
```

4. Build (Optional)

```
go build -o pg_mcp_server ./cmd/server/main.go
./pg_mcp_server
```

## Extension Support

PostGIS ✅  
PgVector ✅  
PgRouting ⭕

## TODO / Unfinished

- Management of tables in the temp schema: A mechanism for table cleanup/recycling should be implemented.
- Unit testing.
