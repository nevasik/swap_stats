## HTTP API + JWT + Rate Limiting

**Минимальные эндпоинты:**

* `GET /health`
* `GET /api/overview` — топ токенов по 5m/1h/24h
* `GET /api/tokens/:id/stats?windows=5m,1h,24h` - получение статистики по переданному токену в заданном интервале

**Безопасность:** JWT (HS256 dev / RS256 prod), проверяем `exp`, `sub`, `aud`.

**Rate limiting:** Redis Token-Bucket

* по **JWT**: 50 rps, burst 200;
* по **IP**: 20 rps, burst 60.

**Почему так:** контролируем доступ и нагрузку, ответы из RAM — p95 ≈ ≤ 80 ms.
