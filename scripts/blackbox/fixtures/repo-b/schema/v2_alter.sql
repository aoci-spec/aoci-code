-- shopfloor schema v2 -- apply after init.sql:
--   mysql -h <host> -u <user> -p <database> < schema/v2_alter.sql

ALTER TABLE customers ADD COLUMN loyalty_tier VARCHAR(16) NOT NULL DEFAULT 'basic';

ALTER TABLE products ALTER INDEX idx_products_name VISIBLE;

CREATE TABLE shipments (
    id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT NOT NULL,
    carrier VARCHAR(64) NOT NULL,
    shipped_at DATETIME(6) NULL,
    UNIQUE KEY uq_shipments_order_carrier (order_id, carrier),
    CONSTRAINT fk_shipments_order FOREIGN KEY (order_id)
        REFERENCES orders (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
