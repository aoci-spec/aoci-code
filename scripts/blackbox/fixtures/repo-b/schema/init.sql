-- shopfloor schema v1 -- MySQL 8.4
-- Database-agnostic on purpose: no CREATE DATABASE, no USE. The operator
-- provisions a schema and applies this file into it:
--   mysql -h <host> -u <user> -p <database> < schema/init.sql

CREATE TABLE customers (
    id INT NOT NULL AUTO_INCREMENT,
    email VARCHAR(190) NOT NULL,
    full_name VARCHAR(120) NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_customers_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE products (
    id INT NOT NULL AUTO_INCREMENT,
    sku VARCHAR(64) NOT NULL,
    name VARCHAR(120) NOT NULL,
    price DECIMAL(10,2) NOT NULL CHECK (price >= 0),
    price_cents INT GENERATED ALWAYS AS (ROUND(price * 100)) STORED,
    name_upper VARCHAR(120) GENERATED ALWAYS AS (UPPER(name)) VIRTUAL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_products_sku (sku),
    KEY idx_products_sku_prefix (sku(8)),
    KEY idx_products_name (name) INVISIBLE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE orders (
    id INT NOT NULL AUTO_INCREMENT,
    customer_id INT NOT NULL,
    external_ref VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'new',
    total DECIMAL(12,2) NOT NULL CHECK (total >= 0),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_orders_customer_ref (customer_id, external_ref),
    KEY idx_orders_created_desc (created_at DESC),
    CONSTRAINT fk_orders_customer FOREIGN KEY (customer_id)
        REFERENCES customers (id) ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE order_items (
    order_id INT NOT NULL,
    line_no INT NOT NULL,
    product_id INT NOT NULL,
    qty INT NOT NULL CHECK (qty > 0),
    unit_price DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (order_id, line_no),
    CONSTRAINT fk_items_order FOREIGN KEY (order_id)
        REFERENCES orders (id) ON DELETE CASCADE,
    CONSTRAINT fk_items_product FOREIGN KEY (product_id)
        REFERENCES products (id) ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Append-only audit stream. Partitioned tables cannot carry foreign
-- keys, so event payloads reference other rows only inside the JSON.
CREATE TABLE events (
    id BIGINT NOT NULL AUTO_INCREMENT,
    event_year SMALLINT NOT NULL,
    kind VARCHAR(32) NOT NULL,
    payload JSON,
    PRIMARY KEY (id, event_year)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
PARTITION BY RANGE (event_year) (
    PARTITION p2024 VALUES LESS THAN (2025),
    PARTITION p2025 VALUES LESS THAN (2026),
    PARTITION pmax VALUES LESS THAN MAXVALUE
);
