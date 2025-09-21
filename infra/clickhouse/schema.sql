CREATE DATABASE IF NOT EXISTS swaps;
USE swaps;

-- Сырьё (raw) + TTL перенос в S3 через 7 дней (на dev удобно быстрее увидеть движение)
-- В проде поменять горизонт на 30/90 дней
CREATE TABLE IF NOT EXISTS raw_swaps
(
    event_date      Date DEFAULT toDate(event_time),
    event_time      DateTime('UTC'),
    ingested_time   DateTime('UTC') DEFAULT now(),
    chain_id        UInt32,
    tx_hash         FixedString(66),
    log_index       UInt32,
    event_id        String,
    token_address   FixedString(42),
    token_symbol    LowCardinality(String),
    pool_address    FixedString(42),
    side            LowCardinality(String),
    amount_token    Decimal(38,18),
    amount_usd      Decimal(20,6),
    block_number    UInt64,
    removed         UInt8,
    schema_version  UInt16
    )
    ENGINE = MergeTree
    PARTITION BY toYYYYMM(event_date)
    ORDER BY (chain_id, token_address, event_time, tx_hash, log_index)
    TTL event_time + INTERVAL 7 DAY TO VOLUME 'cold',
    event_time + INTERVAL 365 DAY DELETE
SETTINGS storage_policy = 'hot_to_cold', index_granularity = 8192;

-- Суточные агрегаты
CREATE TABLE IF NOT EXISTS daily_token_agg
(
    day            Date,
    chain_id       UInt32,
    token_address  FixedString(42),
    token_symbol   LowCardinality(String),
    volume_usd     Decimal(20,6), -- суммарный объем
    trades         UInt64 -- общее количество сделок
    )
    ENGINE = SummingMergeTree
    PARTITION BY toYYYYMM(day)
    ORDER BY (day, chain_id, token_address);

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_daily_token_agg
TO daily_token_agg AS
SELECT
    toDate(event_time) AS day,
    chain_id,
    token_address,
    anyLast(token_symbol) AS token_symbol,
    sumIf(toDecimal64(amount_usd, 6), removed = 0) AS volume_usd,
    countIf(removed = 0) AS trades
FROM raw_swaps
GROUP BY day, chain_id, token_address;
