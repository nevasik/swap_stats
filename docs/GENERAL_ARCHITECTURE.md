                ┌────────────────────────────────────────────────────────────────┐
                │                   ИСТОЧНИКИ СОБЫТИЙ (внешние)                 │
                │  • Индексер on-chain (EVM logs)  • Биржевые источники/адаптеры│
                └───────────────┬────────────────────────────────────────────────┘
                                │  JSON-события Swap (at-least-once доставка)
                                ▼
                    ┌───────────────────────┐   Kafka API
                    │   Redpanda (Kafka)    │◄━━━━━━━━━━━━━━━━┐
                    │   topic: raw-swaps    │                 │
                    └──────────┬────────────┘                 │
                               │ Consumer Group               │
                               ▼                              │(авто-создание топика/retention)
                  ┌──────────────────────────────┐
                  │     Aggregator (наш svc)     │
                  │  ───────────────────────────  │
                  │  1) Dedup (Redis SETNX)       │
                  │  2) Window Engine (5m/1h/24h) │
                  │  3) Snapshot+Offsets (Redis)  │
                  │  4) ClickHouse batch writer   │
                  │  5) WS Hub + NATS broadcast   │
                  └──────┬───────────┬────────────┘
                         │           │
          «горячий state»│           │«срезы/патчи в реал-тайме»
                     ┌───▼───┐     ┌─▼─────────────────────────┐
                     │ Redis │     │           NATS            │
                     │  AOF  │     │ ws.broadcast.token.*      │
                     └───┬───┘     └───────────┬───────────────┘
                         │                    fan-out
                 snapshots+offsets               │
                         │                       │
                         │                ┌──────▼──────────┐
                         │                │   Другие инсты  │
                         │                │ Aggregator’ы    │
                         │                └───┬─────────────┘
                         │                    │ локальные WS-хабы
                         │                    ▼
                         │          ┌─────────────────────┐
                         │          │ WebSocket клиенты   │  (браузер/моб)
                         │          └─────────────────────┘
                         │
                         │ batched raw → аналитика/архив
                         ▼
               ┌──────────────────────────────┐
               │          ClickHouse          │
               │  raw_swaps + MV daily_agg    │  (TTL → S3)
               └───────┬──────────────────────┘
                       │  холодное хранение (через диск=’s3’)
                       ▼
               ┌──────────────────────────────┐
               │       S3 (MinIO, бакет)      │
               │        ч/з storage.xml       │
               └──────────────────────────────┘

         ┌──────────────────────────────────────────────────────────────────┐
         │         HTTP API (chi) + JWT(RS256) + RateLimit (Redis)          │
         │  /healthz /api/overview /api/tokens/:id/stats → из Window Engine │
         └──────────────────────────────────────────────────────────────────┘

         Метрики: Prometheus endpoint, pprof; Логи: stdout/driver

## Поток данных — шаг за шагом

1. Источник (индексер/адаптер) формирует JSON события swap и пишет в Redpanda (Kafka API) в топик raw-swaps.
Retention в топике — «горячий журнал» (дни/недели).

2. Aggregator (наш сервис) читает raw-swaps как consumer group:
   - парсит JSON → SwapEvent и строит канонический id event_id = chain:tx:logIndex; 
   - дедуп в Redis по SETNX с TTL (обычно 24h) — чтобы не учитывать дубликаты; 
   - применяет событие в окна (кольцевые минутные бакеты + rolling 5m/1h/24h) с учётом grace; 
   - батчем пишет «сырьё» в ClickHouse (raw_swaps); 
   - формирует патчи (срезы по токенам) и:
     - раздаёт их своим WebSocket-клиентам через локальный WS-hub (с коалесингом ~100 мс),
     - публикует те же патчи в NATS (ws.broadcast.token.*) — чтобы другие инстансы мгновенно раздали их своим клиентам.

3. Каждые несколько секунд Aggregator делает Snapshot горячего состояния (окна) + Offsets консьюмера в Redis.
При рестарте: Restore() → стартуем "тёплыми" (без перемалывания суток истории).

4. ClickHouse хранит «сырьё» и поддерживает материализованную витрину суточных агрегатов. По DDL настроен TTL:
   - горячие партиции остаются на локальном диске, 
   - старые — перемещаются на S3 (MinIO) в бакет (это наше «холодное хранилище»), 
   - ещё старше — удаляются (задаем в TTL).

5. HTTP API (chi) читает in-memory окна из Aggregator и отдаёт /overview и /tokens/:id/stats.
Доступ защищён JWT RS256 (публичный ключ у сервиса), поверх — rate-limit (Redis token-bucket по sub и IP).

6. Мониторинг: Prometheus-метрики (ingest/apply/lag/ws/ClickHouse batch и т.п.), pprof для профилирования.

## Что хранится и где
- Redpanda: только поток событий (временный журнал, retention); для пересчётов/реплея. 
- Redis:
  - dedupe:* — маркеры увиденных event_id (TTL ~24h),
  - snap:* — снапшот окон и оффсеты консьюмера (для тёплого старта), 
  - rl:* — токены rate-limit (JWT/IP).
- NATS: ничего долговременного — только эфемерные сообщения-патчи для fan-out между инстансами.
- ClickHouse:
  - raw_swaps — «сырьё» (90 дней, например),
  - daily_token_agg — суточная витрина (материализованная),
  - старшИе партиции → S3 (MinIO) согласно storage.xml + TTL.
- S3 (MinIO): «холодные» части партиций ClickHouse — дешёвое долговременное хранение.

## Два ключевых «пути»
 - Real-time путь: Redpanda → Aggregator → окна → WS-клиенты (+ NATS для мульти-инст).
 - Хранилище/аналитика: Redpanda → Aggregator → ClickHouse → (TTL) → S3.

## Безопасность и управление нагрузкой
- JWT RS256 на HTTP/WS: проверяем подпись публичным ключом; приватник лежит на стороне твоего Auth-issuer’а. 
- Rate-limit: Redis token-bucket (по sub и по IP) — защита API/WS от «шумных» клиентов. 
- Grace (на уровне окон): принимает слегка «запаздавшие» события без поломки real-time.

## Восстановление и «ровно-один-раз»
- At-least-once из Redpanda + дедуп в Redis ⇒ по факту учитываем один раз. 
- Snapshot + offsets ⇒ после рестарта состояние и позиция чтения согласованы.

### Метчинг по архитектуре:
1. ingest/ — Kafka consumer (franz-go/sarama).
2. window/ — кольцевые буферы и rolling 5m/1h/24h.
3. stores/clickhouse — коннект + батчер записи.
4. stores/redis — offsets/snapshot + dedup.
5. pubsub/nats — fan-out между инстансами.
6. api/http & api/ws — публичные API и WebSocket.
7. security/ — JWT (RS256 verify).
8. infra/ — docker-компоуз, ClickHouse storage.xml, MinIO, NATS конфиг.
