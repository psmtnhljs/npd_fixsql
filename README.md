<div align="center">
  <img src="docs/nodepassdash-logo.svg" alt="NodePassDash" height="80">
</div>

# NodePassDash (PostgreSQL Edition)

This repository is a PostgreSQL-focused fork of NodePassDash.

It keeps the original NodePassDash dashboard experience, but replaces the previous SQLite runtime path with a full PostgreSQL-based deployment model for long-running production use.

## What Changed

- Fully migrated the backend database layer from SQLite to PostgreSQL
- Removed SQLite runtime dependencies and connection logic
- Added PostgreSQL connection config with `DATABASE_URL` priority and split env fallback
- Rewrote raw SQL that depended on SQLite/MySQL syntax
- Updated JSON field queries to PostgreSQL `jsonb`
- Updated cleanup/maintenance logic for PostgreSQL
- Updated Docker and deployment examples for PostgreSQL
- Built and embedded the real frontend `dist` into the backend

## Database Configuration

The app now supports both of the following:

### Option 1: `DATABASE_URL`

```bash
DATABASE_URL=postgres://nodepass:strong_password@127.0.0.1:5432/nodepassdash?sslmode=disable
```

### Option 2: Split environment variables

```bash
DB_HOST=127.0.0.1
DB_PORT=5432
DB_USER=nodepass
DB_PASSWORD=strong_password
DB_NAME=nodepassdash
DB_SSLMODE=disable
DB_TIMEZONE=Asia/Shanghai
```

If both are present, `DATABASE_URL` takes priority.

## Quick Files

- Docker deployment: `docker-compose.cloud.yml`
- One-click deploy script: `scripts/deploy_cloud_postgres.sh`
- Dev compose: `docker-compose-dev.yml`

## Deploy on a Cloud Server

Recommended target:

- Ubuntu 22.04 / 24.04
- 1 vCPU / 2 GB RAM minimum
- Public TCP port `3000` open, or put it behind Nginx / Caddy

### Method 1: One-click deployment

Run this on your cloud server:

```bash
curl -fsSL https://raw.githubusercontent.com/psmtnhljs/npd_fixsql/main/scripts/deploy_cloud_postgres.sh | sudo bash
```

You can also customize it:

```bash
APP_PORT=3000 \
INSTALL_DIR=/opt/npd_fixsql \
POSTGRES_DB=nodepassdash \
POSTGRES_USER=nodepass \
POSTGRES_PASSWORD='ChangeThisPassword!' \
curl -fsSL https://raw.githubusercontent.com/psmtnhljs/npd_fixsql/main/scripts/deploy_cloud_postgres.sh | sudo -E bash
```

After deployment:

- App directory: `/opt/npd_fixsql`
- Logs directory: `/opt/npd_fixsql/logs`
- Compose file: `/opt/npd_fixsql/docker-compose.cloud.yml`
- Env file: `/opt/npd_fixsql/.env`

Open:

```text
http://YOUR_SERVER_IP:3000
```

### Method 2: Manual deployment

#### 1. Prepare the server

```bash
sudo apt update
sudo apt install -y git curl
```

Install Docker if needed, then clone the repo:

```bash
git clone https://github.com/psmtnhljs/npd_fixsql.git
cd npd_fixsql
```

#### 2. Create `.env`

```bash
cat > .env <<'EOF'
APP_PORT=3000
POSTGRES_DB=nodepassdash
POSTGRES_USER=nodepass
POSTGRES_PASSWORD=ChangeThisPassword!
DB_SSLMODE=disable
DB_TIMEZONE=Asia/Shanghai
LOG_LEVEL=INFO
EOF
```

#### 3. Start services

```bash
docker compose -f docker-compose.cloud.yml up -d --build
```

#### 4. Check status

```bash
docker compose -f docker-compose.cloud.yml ps
docker compose -f docker-compose.cloud.yml logs -f
```

## Common Operations

### Upgrade

```bash
cd /opt/npd_fixsql
git pull
docker compose -f docker-compose.cloud.yml up -d --build
```

### Restart

```bash
cd /opt/npd_fixsql
docker compose -f docker-compose.cloud.yml restart
```

### Stop

```bash
cd /opt/npd_fixsql
docker compose -f docker-compose.cloud.yml down
```

### Backup PostgreSQL

```bash
cd /opt/npd_fixsql
docker compose -f docker-compose.cloud.yml exec -T postgres \
  pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB" > backup.sql
```

### Restore PostgreSQL

```bash
cd /opt/npd_fixsql
cat backup.sql | docker compose -f docker-compose.cloud.yml exec -T postgres \
  psql -U "$POSTGRES_USER" "$POSTGRES_DB"
```

## Notes

- This fork is PostgreSQL-only at runtime
- Existing SQLite data is **not** automatically imported
- For HTTPS in production, it is recommended to place the app behind Nginx or Caddy

## License

BSD-3-Clause. See `LICENSE`.
