# Development

## Prerequisites

- Node.js 20+
- pnpm 8+
- Go 1.23+
- PostgreSQL 14+

## Environment

NodePassDash supports both:

- `DATABASE_URL`
- Split variables: `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`, `DB_TIMEZONE`

`DATABASE_URL` has priority when both are present.

## Dev Mode

```bash
pnpm dev:back
pnpm dev:front
```

## Production Build

```bash
pnpm build
go build -o nodepassdash ./cmd/server
```
