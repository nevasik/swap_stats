## HTTP API + JWT + Rate Limiting

### Публичные эндпоинты (без аутентификации)

* `GET /healthz` — проверка работоспособности сервиса
* `GET /readiness` — проверка готовности всех зависимостей (Redis, ClickHouse, NATS)
* `GET /metrics` — метрики Prometheus
* `POST /auth/mint-token` — **[DEV ONLY]** генерация JWT токена для тестирования

### Защищённые эндпоинты (требуют JWT + Rate Limiting)

* `GET /api/overview` — топ токенов по 5m/1h/24h
* `GET /api/tokens/:id/stats?windows=5m,1h,24h` - получение статистики по переданному токену в заданном интервале

---

### Аутентификация и авторизация

**JWT токены:** RS256 (асимметричная криптография)
- Проверяем: `exp` (время истечения), `sub` (субъект), `aud` (аудитория), `iss` (издатель)
- Публичный ключ для верификации: настраивается через `config.yaml`

**Получение токена для тестирования:**

```bash
curl -X POST http://localhost:8080/auth/mint-token \
  -H "Content-Type: application/json" \
  -d '{
    "subject": "user123",
    "ttl": 3600000000000
  }'
```

**Ответ:**
```json
{
  "token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "subject": "user123",
  "ttl": "1h0m0s",
  "expires_at": 1234567890
}
```

**Параметры запроса:**
- `subject` (обязательный) — идентификатор пользователя/системы
- `ttl` (опциональный) — время жизни токена в наносекундах (по умолчанию: 1 час = 3600000000000)
- `id` (опциональный) — уникальный идентификатор токена (jti claim)
- `extra` (опциональный) — дополнительные custom claims

**Использование токена:**
```bash
TOKEN="eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."

curl http://localhost:8080/api/overview \
  -H "Authorization: Bearer $TOKEN"
```

⚠️ **ВАЖНО:** Endpoint `/auth/mint-token` предназначен только для разработки и тестирования. В production он должен быть отключен или защищён дополнительной аутентификацией!

---

### Rate Limiting

**Двухуровневое ограничение запросов:** Redis Token-Bucket

1. **По JWT токену (subject):**
   - Скорость пополнения: 50 токенов/сек
   - Burst: 200 запросов
   - TTL: 2 минуты

2. **По IP адресу:**
   - Скорость пополнения: 20 токенов/сек
   - Burst: 60 запросов
   - TTL: 2 минуты

**Заголовки ответа:**
```
X-RateLimit-Limit-IP: 60
X-RateLimit-Remaining-IP: 58
X-RateLimit-Limit-JWT: 200
X-RateLimit-Remaining-JWT: 195
Retry-After: 1
```

**При превышении лимита:**
```json
{
  "error": {
    "code": "rate_limit_exceeded",
    "message": "too many requests",
    "details": {}
  }
}
```

**Извлечение IP:** Поддержка trusted proxies с CIDR нотацией. Приоритет:
1. `X-Forwarded-For` (если proxy доверенный)
2. `X-Real-IP`
3. `RemoteAddr`

**Fail-open стратегия:** При отказе Redis запросы пропускаются, чтобы не блокировать сервис.

---

**Почему так:** контролируем доступ и нагрузку, ответы из RAM — p95 ≈ ≤ 80 ms.
