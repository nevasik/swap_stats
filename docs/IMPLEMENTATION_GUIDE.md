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
