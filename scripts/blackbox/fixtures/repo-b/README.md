# shopfloor

Order-management service for the shop floor: a FastAPI application over
MySQL 8.4. It registers customers, keeps a small product catalog with
server-generated pricing columns, and places orders with volume-discounted
line items guarded by an in-process stock reservation ledger.

## Layout

- `app/` — application package (settings, DB helpers, models, repositories,
  services, HTTP routers).
- `schema/` — hand-maintained DDL, applied with the `mysql` client.
- `tests/` — pytest unit tests for the pure domain services.
- `data/` — operational exports and site assets used by support tooling.
- `docs/` — architecture notes.

## Database setup

The service never creates or selects its own database: `schema/init.sql`
is database-agnostic (no `CREATE DATABASE`, no `USE`), so the operator
provisions a schema first and applies the DDL into it explicitly:

```sh
mysql -h db.internal -u shopfloor -p shopfloor_prod < schema/init.sql
```

Later drift (v2) is applied the same way, after v1 is in place:

```sh
mysql -h db.internal -u shopfloor -p shopfloor_prod < schema/v2_alter.sql
```

All tables are InnoDB with `utf8mb4`. The `products` table carries two
generated columns (`price_cents` STORED, `name_upper` VIRTUAL) that the
application mirrors in `app/services/pricing.py`; the `events` table is
range-partitioned by `event_year`.

## Configuration

Configuration is read from the environment at startup
(`app/settings.py`):

- `SHOP_DATABASE_DSN` — `mysql://user:password@host:3306/database`
  (required in production; a localhost default exists for development).
- `SHOP_BIND_HOST` / `SHOP_BIND_PORT` — HTTP bind address, default
  `0.0.0.0:8000`.

## Running

```sh
pip install -e .[dev]
export SHOP_DATABASE_DSN='mysql://shopfloor:secret@db.internal:3306/shopfloor_prod'
uvicorn app.main:app --host 0.0.0.0 --port 8000
```

The lifespan hook pings MySQL once before the server accepts traffic, so
a bad DSN fails fast instead of surfacing as request errors.

Run the unit tests (no database needed) with:

```sh
pytest
```
