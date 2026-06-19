# 开发环境

## 环境准备

- Node.js 20+
- pnpm 8+
- Go 1.23+
- PostgreSQL 14+

## 数据库配置

支持两种方式：

- `DATABASE_URL`
- 拆分变量：`DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME`、`DB_SSLMODE`、`DB_TIMEZONE`

如果同时存在，`DATABASE_URL` 优先。

## 开发模式

```bash
pnpm dev:back
pnpm dev:front
```

## 生产构建

```bash
pnpm build
go build -o nodepassdash ./cmd/server
```
