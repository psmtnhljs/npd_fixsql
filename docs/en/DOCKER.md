# Docker Deployment (NodePassDash)

This guide deploys NodePassDash with PostgreSQL by using Docker Compose.

## Requirements

- Docker Engine + Docker Compose
- A directory for `logs/`

## Quick Start

1) Create a working directory:

```bash
mkdir -p nodepassdash && cd nodepassdash
mkdir -p logs
```

2) Use the repo `docker-compose.yml`, or copy this example:

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

3) Start:

```bash
docker compose up -d
```

## Database Configuration

NodePassDash supports both:

- `DATABASE_URL`
- Split variables: `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`, `DB_TIMEZONE`

If both are set, `DATABASE_URL` takes priority.

## Backup / Restore

- Backup PostgreSQL data with `pg_dump`, or back up the Compose PostgreSQL volume.
- Back up `logs/` separately if needed.

## Troubleshooting

- Health check: `curl -fsS http://localhost:3000/api/health`
- App logs: `docker logs -f nodepassdash`
- Database logs: `docker logs -f nodepassdash-postgres`
