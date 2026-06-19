# Docker 部署（NodePassDash）

本指南使用 Docker Compose 部署基于 PostgreSQL 的 NodePassDash。

## 环境要求

- Docker Engine + Docker Compose
- `logs/` 日志目录

## 快速开始

1）准备目录：

```bash
mkdir -p nodepassdash && cd nodepassdash
mkdir -p logs
```

2）直接使用仓库内的 `docker-compose.yml`，或参考下面的示例：

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: nodepassdash
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres

  nodepassdash:
    image: ghcr.io/nodepassproject/nodepassdash:latest
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "3000:3000"
    environment:
      DATABASE_URL: postgres://postgres:postgres@postgres:5432/nodepassdash?sslmode=disable&TimeZone=Asia%2FShanghai
    volumes:
      - ./logs:/app/logs
```

3）启动：

```bash
docker compose up -d
```

## 数据库配置

支持两种方式：

- `DATABASE_URL`
- 拆分变量：`DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME`、`DB_SSLMODE`、`DB_TIMEZONE`

如果两种都配置，`DATABASE_URL` 优先。

## 备份与恢复

- 数据库建议使用 `pg_dump` 备份，或直接备份 PostgreSQL 数据卷。
- `logs/` 需要单独备份。

## 排错

- 健康检查：`curl -fsS http://localhost:3000/api/health`
- 应用日志：`docker logs -f nodepassdash`
- 数据库日志：`docker logs -f nodepassdash-postgres`
