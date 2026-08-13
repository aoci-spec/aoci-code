# shopfloor architecture

## Module layers

Requests flow top to bottom; nothing lower ever imports upward.

| Layer | Modules | Responsibility |
| --- | --- | --- |
| HTTP | `app/api/health.py`, `app/api/customers.py`, `app/api/orders.py` | wire types, status-code mapping |
| Domain | `app/services/pricing.py`, `app/services/inventory.py` | discount tiers, stock holds; no I/O |
| Persistence | `app/repositories/*` | parameterized SQL, dataclass mapping |
| Infrastructure | `app/settings.py`, `app/db.py` | DSN parsing, cursors, retry-once connect |

`app/main.py` assembles the layers: it builds `Settings` from the
environment, attaches one `InventoryLedger` to `app.state`, mounts the
routers, and pings MySQL in the lifespan hook before serving.

## Schema overview (v1, `schema/init.sql`)

Five tables, all InnoDB / `utf8mb4`:

- **customers** — identity per unique `email`; referenced by orders.
- **products** — catalog keyed by unique `sku`; two generated columns
  (`price_cents` STORED as `ROUND(price*100)`, `name_upper` VIRTUAL), a
  prefix index on `sku(8)`, and an INVISIBLE index on `name` kept warm
  for the search feature.
- **orders** — header rows; `(customer_id, external_ref)` unique for
  idempotent upstream retries, a DESC index on `created_at` for the
  "latest orders" screen, FK to customers with `ON DELETE CASCADE`.
- **order_items** — lines keyed by `(order_id, line_no)`; FKs to orders
  (`CASCADE`) and products (`ON UPDATE CASCADE`).
- **events** — append-only audit stream, `PRIMARY KEY (id, event_year)`
  and RANGE-partitioned by `event_year` (p2024 / p2025 / pmax) so old
  years can be dropped by partition.

## Schema drift (v2, `schema/v2_alter.sql`)

Three statements, applied after v1 in every environment:

1. `customers.loyalty_tier` column (default `'basic'`) for the loyalty
   programme.
2. `idx_products_name` flipped VISIBLE now that name search shipped.
3. New `shipments` table, one row per `(order_id, carrier)`, FK back to
   orders with `ON DELETE RESTRICT` so shipped orders cannot vanish.

## Consistency contracts

- `pricing.to_price_cents` must round half-up exactly like the
  `price_cents` generated column; `tests/test_pricing.py` pins this.
- `orders.total` is computed by `pricing.order_total` from discounted
  lines and satisfies the `CHECK (total >= 0)` constraint.
- The inventory ledger is process-local by design: it bounds bursts on
  one instance and is rebuilt empty on restart; the database remains
  the source of truth for durable state.
