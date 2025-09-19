# Swap Stats — Implementation Guide

# Автор: **telegram - @nevasik**

> Цель: пошагово, понятно построить сервис агрегации свопов в реальном времени (1000 EPS), с низкой задержкой, High Availability, дедупом, скользящими окнами, снапшотами и выдачей через HTTP/WS.
> По умолчанию используем **Redpanda** (Kafka API), но оставляем быстрый переключатель на **Apache Kafka**.

---

## Содержание
1. [ТЗ](#тз)
2. [Обзор](#обзор)
3. [Технологический стек](#технологический-стек)
4. [Глоссарий (термины простыми словами)](#глоссарий-термины-простыми-словами)
5. [Архитектура и потоки данных](#архитектура-и-потоки-данных)
6. [Локальный стенд (Docker Compose)](#локальный-стенд-docker-compose)
7. [Конфигурация сервиса (`config.yaml`)](#конфигурация-сервиса-configyaml)
8. [Данные и доменные модели](#данные-и-доменные-модели)
9. [Дедупликация (Dedup)](#дедупликация-dedup)
10. [Оконный движок: кольцевые буферы + rolling-суммы + grace](#оконный-движок-кольцевые-буферы--rolling-суммы--grace)
11. [Снапшоты (Snapshot/Restore)](#снапшоты-snapshotrestore)
12. [Ingest из Redpanda/Kafka](#ingest-из-redpandakafka)
13. [Долговременное хранение: ClickHouse + суточные MV](#долговременное-хранение-clickhouse--суточные-mv)
14. [HTTP API + JWT + Rate Limiting](#http-api--jwt--rate-limiting)
15. [WebSocket + коалесинг + кластерный фан-аут (NATS)](#websocket--коалесинг--кластерный-фан-аут-nats)
16. [Наблюдаемость: метрики и pprof](#наблюдаемость-метрики-и-pprof)
17. [Переключение Redpanda ⇄ Kafka](#переключение-redpanda--kafka)
18. [Тест-план (unit/integration/load)](#тест-план-unitintegrationload)
19. [Runbook (операционные правила)](#runbook-операционные-правила)
20. [Быстрый старт (команды)](#быстрый-старт-команды)

--- 
## ТЗ
У вас есть 1000 свопов в секунду от производителя (кто, токен, сумма, доллар США, сторона и т.д).
Производитель также сохраняет эти данные в базе данных.

Необходимо создать систему, которая вычисляет статистику токенов в реальном времени(объем за 5 минут, объем за 1 час, объем за 24 часа, количество транзакций и т.д)
и обслуживает эти данные через HTTP API и обновления WebSocket с минимальной задержкой.

Система должна быть высокодоступной и обрабатывать перезапуски без потери данных или пропуска событий во время запуска.
Она должна быть масштабируемой, чтобы мы могли запускать больше экземпляров.
Данные свопов могут содержать дубликаты, а порядок блоков не гарантируется.

1. Теоретическая задача. Спроектируйте полную архитектуру.
- Какие транспортные механизмы вы бы использовали от производителя?
- Где вы бы хранили различные типы данных?
- Как вы бы обеспечили высокую доступность и отсутствие потерь данных?

2. Практическая задача. Реализуйте сервис Go, который:
- считывает события свопа из канала
- вычисляет статистику
- обслуживает данные по HTTP
- отправляет обновления в канал WebSocket
- обрабатывает перезапуски.
- Используйте интерфейсы для хранения.

![img.png](img.png)
---

## Обзор

Мы строим сервис, который:

* принимает \~**1000 событий/сек** (EPS),
* считает **скользящие окна** по токенам: 5m / 1h / 24h,
* **отбрасывает дубликаты** (дедуп),
* корректно обрабатывает **опоздавшие события** (grace),
* хранит сырые данные в **ClickHouse** 90 дней,
* отдаёт состояние через **HTTP** и **WebSocket** с минимальной задержкой,
* переживает рестарты без перерасчёта суток — через **снапшоты**,
* масштабируется горизонтально, High Availability.

---

## Технологический стек

* **Брокер:** Redpanda (Kafka API). Возможность переключения на Apache Kafka.
* **Хранилища:** Redis (дедуп, снапшоты, rate-limit), ClickHouse (raw), NATS (кластерный фан-аут для WS).
* **Cold storage:** S3/MinIO (ClickHouse storage policy: HOT → COLD(S3)).
* **Ядро:** Go 1.22+, chi (HTTP), nhooyr/websocket (WS), собственный логгер - обертка(zerolog) + alert manager to telegram API.
* **Метрики:** Prometheus, pprof.

---

## Глоссарий

* **Дедуп (deduplication):** фильтр дублей. Одно и то же событие учитываем **ровно один раз**.
* **Кольцевой буфер (ring):** массив фиксированного размера, идущий “по кругу” — индексируем минуту `minute % size`.
* **Окна (5m/1h/24h):** метрики “за последние N минут/часов/суток”.
  **Rolling-суммы** позволяют отдавать их за O(1), без пересчёта.
* **Grace:** допуск опоздания событий (например, 120 секунд).
* **Снапшот:** “фото RAM-состояния” + текущие offsets стрима, чтобы **тёпло** стартовать после рестарта.
* **At-least-once:** доставка может дублироваться → обязательно нужен дедуп.

---

## Архитектура и потоки данных

1. **Ingest** (Consumer Group) читает события из Redpanda/Kafka.
2. **Dedup** (Redis SETNX/TTL) отбрасывает дубли по `event_id`.
3. **Оконный движок** применяет ивент в соответствующий **минутный бакет** (ring) и обновляет **rolling** 5m/1h/24h.
   Учитываем **grace** для поздних событий.
4. **Снапшот** состояния + offsets каждые 5s в Redis → быстрый рестарт.
5. **WS push**: локальный хаб → коалесинг 100ms → клиенты; меж-инстансовый фан-аут через **NATS**.
6. **ClickHouse**: асинхронная запись “сырья” батчами; суточные MV для отчётов.
7.  **S3**: заложим холодное хранение из ClickHouse в bucket S3.
---

## Локальный стенд (Docker Compose)

Поднимаем **Redpanda, Redis, NATS, ClickHouse**. Redpanda — дефолт.
(Можно поднять Apache Kafka, код не меняется)

Мини-фрагмент:

```yaml
services:
  redpanda:
    image: redpandadata/redpanda:v24.1.7
    command: >
      redpanda start --overprovisioned --smp 1 --memory 1G --reserve-memory 0M
      --node-id 0 --check=false --set redpanda.auto_create_topics_enabled=true
    ports: ["9092:9092"]

  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]

  nats:
    image: nats:2.10-alpine
    command: -js
    ports: ["4222:4222"]

  clickhouse:
    image: clickhouse/clickhouse-server:23.8
    environment:
      - CLICKHOUSE_DB=swaps
      # --- S3 creds (в проде вынести в secrets/ vault) ---
      - AWS_REGION=eu-central-1
      - S3_BUCKET=ch-swaps-cold
      - S3_ACCESS_KEY=CHANGE_ME
      - S3_SECRET_KEY=CHANGE_ME
    ports: ["9000:9000","8123:8123"]
    volumes:
      - ./clickhouse_data:/var/lib/clickhouse
      - ./clickhouse/config.d/storage.xml:/etc/clickhouse-server/config.d/storage.xml:ro
```

---

## Конфигурация сервиса (`config.yaml`)

Переключатель **Redpanda ⇄ Kafka** (меняем только `broker_type` и `brokers`):

```yaml
ingest:
  broker_type: redpanda           # redpanda | kafka
  brokers: ["localhost:9092"]
  topic: "raw-swaps"
  group_id: "swap-stats"
  start: "latest"                 # или "earliest"
  max_bytes: 1048576
  topic_cfg:
    partitions: 12
    replication: 1                # dev; в проде 3
    retention_ms: 604800000       # 7d
    cleanup_policy: delete
    segment_ms: 3600000           # 1h

app:
  grace: "120s"
  snapshot_interval: "5s"
  dedupe_ttl: "24h"

stores:
  redis:
    addr: "localhost:6379"
    db: 2
    prefix: "swapstats:"
  clickhouse:
    dsn: "tcp://localhost:9000?database=swaps&compress=true"

pubsub:
  nats:
    url: "nats://localhost:4222"

security:
  jwt:
    enabled: true
    alg: "HS256"
    secret: "CHANGE_ME"

rate_limit:
  by_jwt: { refill_per_sec: 50, burst: 200 }
  by_ip:  { refill_per_sec: 20, burst: 60 }

api:
  http: { addr: ":8080" }
  ws:   { coalesce_ms: 100, max_conn: 5000 }

metrics:
  prometheus: ":9091"
  pprof: ":6060"
```

**Почему так:**
Redpanda/Kafka — один API. Вся разница — адреса и админка. Остальной стек — неизменен.

---

## Данные и доменные модели

Канонический ID события: `event_id = "<chain_id>:<tx_hash>:<log_index>"`.

```go
type Side string
const (
  SideBuy  Side = "buy"
  SideSell Side = "sell"
)

type SwapEvent struct {
  EventID      string
  ChainID      uint64
  TxHash       string
  LogIndex     uint
  Token        string   // addr/symbol
  AmountTok    float64
  AmountUSD    float64
  Side         Side
  EventTime    time.Time
  IngestedTime time.Time
  Removed      bool
}

type Agg struct {
  VolumeUSD  float64
  VolumeTok  float64
  Trades     int64
  BuyTrades  int64
  SellTrades int64
}
```

---

## Дедупликация (Dedup)

**Зачем:** at-least-once доставка → возможны повторы. Без дедупа статистика “уплывёт”.

**Как:** Redis `SETNX` + TTL (например, 24h). Ключ — `dedupe:{event_id}`.

```go
// true => уже видели (дубликат)
func (r *RedisDeduper) Seen(ctx context.Context, id string, ttl time.Duration) (bool, error) {
  ok, err := r.cli.SetNX(ctx, "dedupe:"+id, 1, ttl).Result()
  return !ok, err
}
```

**Почему так:** глобальный дедуп для всех инстансов/после рестартов.

---

## Оконный движок: кольцевые буферы + rolling-суммы + grace

**Кольцевой буфер (ring) из минутных бакетов:**
Размер 1440 (24h×60). Индекс по минуте: `i = (event_time.Unix()/60) % 1440`.
Если бакет в ячейке от другой минуты — обнуляем и пишем новую.

```go
type minuteBucket struct {
  Minute int64 // unix minute
  Agg    Agg
}

type tokenRing struct {
  buckets []minuteBucket           // len = 1440
}

func (r *tokenRing) applyEvent(t time.Time, usd, tok float64, buy bool) (delta Agg, minute int64) {
  m := t.Unix() / 60; i := int(m % 1440)
  b := &r.buckets[i]
  if b.Minute != m { *b = minuteBucket{Minute: m} }
  before := b.Agg
  // обновляем агрегаты бакета:
  b.Agg.VolumeUSD += usd
  b.Agg.VolumeTok += tok
  b.Agg.Trades++
  if buy { b.Agg.BuyTrades++ } else { b.Agg.SellTrades++ }
  // дельта для корректировки rolling:
  delta = b.Agg; delta.VolumeUSD -= before.VolumeUSD; delta.VolumeTok -= before.VolumeTok
  delta.Trades -= before.Trades; delta.BuyTrades -= before.BuyTrades; delta.SellTrades -= before.SellTrades
  return delta, m
}
```

**Rolling-суммы** (O(1) выдача окон):
Храним три суммы (5m/1h/24h). При обновлении минутного бакета добавляем **дельту**; раз в минуту вычитаем то, что “выпало” из окна.

**Grace для поздних событий:**
`watermark = now - grace` (например, 120s). Если `event_time < watermark` — старьё; иначе применяем.

**Почему так:** фиксированная память, O(1)/O(min) операции, корректность при out-of-order в пределах grace.

---

## Снапшоты (Snapshot/Restore)

**Что это:** периодическая сериализация **горячего состояния** (кольца + rolling) + **offsets** стрима в Redis.
**Зачем:** “тёплый старт” — после рестарта не пересчитываем сутки истории, а продолжаем **с нужного места**.

**Когда:** каждые 5s + перед graceful shutdown.

Формат (идея):

```go
type WindowSnapshot struct {
  Version  int
  TakenAt  time.Time
  GraceSec int64
  Tokens   map[string]struct{
    Buckets []struct{ Minute int64; Agg Agg }
    W5m, W1h, W24h Agg
  }
  // Offsets храним рядом тем же ревизионным ключом
}
```

**Алгоритм атомарности:**

1. Save(state) → 2) Save(offsets) с тем же `revision`.
   На старте ищем **последнюю согласованную пару** и восстанавливаем.

**Почему так:** корректная привязка RAM-состояния к позиции в стриме = отсутствие дыр/повторов после рестартов.

---

## Ingest из Redpanda/Kafka

**Пайплайн обработки одного сообщения:**

1. parse JSON → `SwapEvent`
2. **dedup** (Redis SETNX/TTL=24h) → дубликат? пропускаем
3. apply (ring/rolling) с учётом `grace`
4. отдать апдейт в локальный WS-hub + **NATS** (кластерный фан-аут)
5. асинхронно положить в батч **ClickHouse**
6. **commit offset** (после успешного apply)

**Почему так:** at-least-once + идемпотентность → эффективно “ровно один раз” в метриках.

---

## Долговременное хранение: ClickHouse + суточные MV

**Зачем:** хранить сырьё 90 дней, быстрые аналитические запросы, суточные витрины.

DDL (смысл):

```sql
CREATE TABLE IF NOT EXISTS raw_swaps
(
    event_date      Date        DEFAULT toDate(event_time),
    event_time      DateTime64(3, 'UTC'),
    ingested_time   DateTime64(3, 'UTC') DEFAULT now(),
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
    TTL
    event_date + INTERVAL 90 DAY TO VOLUME 'cold',   -- через 90 дней → S3
    event_date + INTERVAL 365 DAY DELETE             -- через 365 дней удалить
SETTINGS storage_policy = 'hot_to_s3', index_granularity = 8192;
```

Материализованная витрина (суточные агрегаты по токену) — для быстрых отчётов.

Пишем батчами: **каждые \~200 ms** или **по 500–1000** записей (что раньше).

---

## Холодное хранение (S3)
**Холодное хранение**: ClickHouse → S3 (tiering)

**Зачем**: NVMe держит “горячие” партиции; через 90 дней данные автоматически переезжают в S3 (дёшево, долговечно). Запросы прозрачны для приложения.

**Как работает**: storage policy hot_to_s3 с дисками hot и s3_cold. TTL ... TO VOLUME 'cold' инициирует переезд.

**Фоновые перемещения выполняет ClickHouse**.

---

## HTTP API + JWT + Rate Limiting

**Минимальные эндпоинты:**

* `GET /healthz`
* `GET /api/overview` — топ токенов по 5m/1h/24h
* `GET /api/tokens/:id/stats?windows=5m,1h,24h`

**Безопасность:** JWT (HS256 dev / RS256 prod), проверяем `exp`, `sub`, `aud`.

**Rate limiting:** Redis Token-Bucket

* по **JWT**: 50 rps, burst 200;
* по **IP**: 20 rps, burst 60.

**Почему так:** контролируем доступ и нагрузку, ответы из RAM — p95 ≈ ≤ 80 ms.

---

## WebSocket + коалесинг + кластерный фан-аут (NATS)

* Библиотека: `nhooyr.io/websocket`.
* Протокол: `{"subscribe":"token:USDC"}`; сервер шлёт **JSON-патчи** только изменённых окон.
* **Коалесинг:** отправка одним пакетом **раз в 100 ms** (10 Гц).
* **Backpressure:** буфер на клиента; при переполнении — прореживание/закрытие “медленных”.
* **NATS:** меж-инстансовый фан-аут (`ws.broadcast.token.*`), чтобы все поды видели апдейты.

**Почему так:** почти realtime, без лавины сообщений, масштабируемость по инстансам.

---

## Наблюдаемость: метрики и pprof

**Prometheus:**

* `events_ingested_total`, `events_applied_total`
* `dedupe_hits_total`
* `consumer_lag_seconds`
* `window_apply_latency_ms` (hist)
* `snapshot_duration_ms` (hist)
* `ws_clients_gauge`, `ws_dropped_msgs_total`
* `clickhouse_batch_size`, `clickhouse_flush_duration_ms`

**pprof:** `:6060` (CPU/heap/trace) — для поиска узких мест.

---

## Переключение Redpanda ⇄ Kafka

1. В `config.yaml`:

    * `ingest.broker_type: redpanda | kafka`
    * `ingest.brokers: ["redpanda:9092"]` / `["kafka:9092"]`

2. Скрипт `build/kafka/create_or_update_topic.sh` (универсальный):

    * если есть `rpk` → создаёт/чинит топик в Redpanda,
    * иначе — `kafka-topics.sh`/`kafka-configs.sh` для Kafka.

3. В `docker-compose.dev.yml` держим оба варианта (Redpanda — дефолт, Kafka — закомментирован).

**Почему так:** код не меняем вообще; переключение — дело конфигов и админки.

---

## Тест-план (unit/integration/load)

**Unit:**

* Dedup (Redis): Seen/TTL/состязательность.
* Ring/rolling: корректность сумм; выпадающие минуты; границы суток.
* Snapshot/Restore: идентичность состояния до/после.

**Integration:**

* Ingest→Apply→WS→Restart→Restore→продолжить без пропусков/дублей.
* Late events (например, now-90s) корректно попадают и корректируют окна.

**Load:**

* Генератор 1–3k EPS на 10 минут (продюсер/ксатор).
* k6: HTTP sustained 1000 RPS, burst 5000 RPS.
* WS: 1000 клиентов × 5 подписок, коалесинг 100 ms → \~50k msg/s.

**HA:**

* 2 инстанса: убить один → без потерь, лаг ≈ 0, WS-фан-аут через NATS работает.

---

## Runbook (операционные правила)

* **Рестарт:** всегда graceful → финальный Snapshot; на старте Restore → “тёплый” сервис.
* **Лаг растёт:** проверяем consumer lag, CPU, Redis RTT; добавляем партиции/инстансы.
* **Reorg:** `removed=true` → применяем **компенсацию** (вычесть вклад), пушим патч в WS.
* **Retentions:** Redpanda/Kafka — 7 дней (горячий буфер); ClickHouse — 90 дней (дальше cold/S3 → delete).
* **Проверка tiering:** раз в день мониторить system.parts по disk_name='s3_cold' и part_log (MovePart).
* **Аварии S3:** при недоступности S3 запросы к холодным партициям деградируют/ошибаются — алерты по system.events.S3* и логам CH.

---

## Быстрый старт (команды)

**1) Поднять окружение:**

```bash
docker compose -f infra/docker/docker-compose.dev.yml up -d
```

**2) Создать/обновить топик:**

```bash
./infra/kafka/create_or_update_topic.sh \
  raw-swaps 12 604800000 delete 3600000 "localhost:9092"
```

> Скрипт сам определит: Redpanda (`rpk`) или Kafka‐утилиты.

**3) Запустить приложение:**

```bash
CONFIG=cmd/aggregator/config_dev.yaml go run ./cmd/aggregator
```

**4) Проверка:**

* `GET /healthz` — 200 OK
* `GET /api/tokens/USDC/stats?windows=5m,1h,24h` — данные из RAM
* WebSocket `/ws` → `{"subscribe":"token:USDC"}` — патчи идут каждые \~100 ms

---

### Почему эта архитектура надёжна и быстра

* **Идемпотентность** + **at-least-once** = корректные метрики.
* **Ring + rolling** = O(1) время ответа и фиксированная память на токен.
* **Grace** = корректность out-of-order без большой задержки.
* **Snapshot/Restore** = быстрый тёплый старт, минимальный RTO(целевое время восстановления).
* **ClickHouse** = дешёвое и быстрое долгосрочное хранение с готовыми витринами.
* **WS с коалесингом** = realtime без флуда, масштабирование через NATS.

---
