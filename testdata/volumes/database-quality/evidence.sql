-- Synthetic, inert evidence for R67-B2 tests. No production code executes it.
CREATE TABLE country_codes (
  code text PRIMARY KEY,
  display_name text NOT NULL
);

CREATE TABLE customers (
  id bigint PRIMARY KEY,
  tenant_id bigint NOT NULL,
  email text NOT NULL,
  status text NOT NULL,
  created_at timestamp NOT NULL,
  updated_at timestamp NOT NULL
);

-- 24 columns: physical breadth must not expand F/R/A/S.
CREATE TABLE customer_profiles (
  customer_id bigint PRIMARY KEY REFERENCES customers(id),
  legal_name text, preferred_name text, locale text, timezone text,
  phone text, secondary_phone text, address_1 text, address_2 text,
  city text, region text, postal_code text, country_code text,
  marketing_opt_in boolean, support_tier text, risk_class text,
  tax_identifier text, billing_note text, shipping_note text,
  integration_a text, integration_b text, integration_c text,
  created_at timestamp, updated_at timestamp
);

-- 56 columns: this is schema evidence, never content for a long F.
CREATE TABLE orders (
  id bigint PRIMARY KEY, customer_id bigint REFERENCES customers(id),
  order_number text UNIQUE, status text, currency text, total numeric,
  field_07 text, field_08 text, field_09 text, field_10 text,
  field_11 text, field_12 text, field_13 text, field_14 text,
  field_15 text, field_16 text, field_17 text, field_18 text,
  field_19 text, field_20 text, field_21 text, field_22 text,
  field_23 text, field_24 text, field_25 text, field_26 text,
  field_27 text, field_28 text, field_29 text, field_30 text,
  field_31 text, field_32 text, field_33 text, field_34 text,
  field_35 text, field_36 text, field_37 text, field_38 text,
  field_39 text, field_40 text, field_41 text, field_42 text,
  field_43 text, field_44 text, field_45 text, field_46 text,
  field_47 text, field_48 text, field_49 text, field_50 text,
  field_51 text, field_52 text, field_53 text, field_54 text,
  created_at timestamp, updated_at timestamp
);

-- Deliberately many foreign keys: R remains selected rather than exhaustive.
CREATE TABLE order_dependencies (
  order_id bigint REFERENCES orders(id),
  customer_id bigint REFERENCES customers(id),
  country_code text REFERENCES country_codes(code),
  role_customer_id bigint REFERENCES customers(id),
  ref_05 bigint REFERENCES customers(id), ref_06 bigint REFERENCES customers(id),
  ref_07 bigint REFERENCES customers(id), ref_08 bigint REFERENCES customers(id),
  ref_09 bigint REFERENCES customers(id), ref_10 bigint REFERENCES customers(id)
);

CREATE TABLE payments (
  id bigint PRIMARY KEY, order_id bigint REFERENCES orders(id),
  payment_reference text UNIQUE, state text, amount numeric
);

CREATE TABLE audit_events (
  id bigint PRIMARY KEY, subject_type text, subject_id bigint,
  event_type text, payload text, created_at timestamp NOT NULL
);

CREATE TABLE customer_roles (
  customer_id bigint REFERENCES customers(id),
  role_id bigint NOT NULL,
  PRIMARY KEY (customer_id, role_id)
);
