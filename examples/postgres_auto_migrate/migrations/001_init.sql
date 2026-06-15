CREATE TABLE IF NOT EXISTS private_keys_hex (
    id BIGSERIAL PRIMARY KEY,
    private_key_hex TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS scan_progress (
    chain_id BIGINT PRIMARY KEY,
    last_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
