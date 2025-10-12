# Postman Collection для Dexcelerate API

## Быстрый старт

### 1. Импорт коллекции и окружения

1. Откройте Postman
2. Нажмите **Import** (кнопка вверху слева)
3. Перетащите или выберите файлы:
   - `Dexcelerate_API.postman_collection.json`
   - `Dexcelerate_Local.postman_environment.json`
   - `Dexcelerate_Staging.postman_environment.json` (опционально)
4. Нажмите **Import**

### 2. Выбор окружения

В правом верхнем углу Postman выберите окружение:
- **Dexcelerate Local** для локальной разработки (localhost:8080)
- **Dexcelerate Staging** для staging сервера

### 3. Получение JWT токена

1. Откройте папку **Public Endpoints**
2. Запустите запрос **Mint JWT Token**
3. Токен автоматически сохранится в переменную `jwt_token`
4. Теперь можно вызывать защищённые endpoints!

### 4. Вызов защищённых endpoints

Все запросы в папке **Protected Endpoints** автоматически используют сохранённый токен.

Просто запустите любой запрос, например:
- **Get Overview**
- **Get Token Stats**

## Структура коллекции

### 📂 Public Endpoints

Endpoints которые не требуют аутентификации:

| Запрос | Метод | URL | Описание |
|--------|-------|-----|----------|
| Health Check | GET | `/healthz` | Проверка работоспособности |
| Readiness Check | GET | `/readiness` | Проверка всех зависимостей |
| Metrics | GET | `/metrics` | Prometheus метрики |
| **Mint JWT Token** | POST | `/auth/mint-token` | **Получить JWT токен** |
| Mint JWT Token (Custom) | POST | `/auth/mint-token` | Токен с кастомными параметрами |

### 🔒 Protected Endpoints

Endpoints которые требуют JWT аутентификацию:

| Запрос | Метод | URL | Описание |
|--------|-------|-----|----------|
| Get Overview | GET | `/api/overview` | Топ токенов по временным окнам |
| Get Token Stats | GET | `/api/tokens/:id/stats` | Статистика по конкретному токену |
| Get Token Stats (WETH) | GET | `/api/tokens/WETH/stats` | Пример для WETH |

### ⚠️ Error Cases

Примеры ошибочных запросов для тестирования:

| Запрос | Ожидаемый результат |
|--------|---------------------|
| Unauthorized Request | 401 Unauthorized |
| Mint Token (Missing Subject) | 400 Bad Request |
| Rate Limit Test | 429 Too Many Requests (после burst) |

## Автоматические возможности

### 🎯 Автоматическое сохранение токена

Запрос **Mint JWT Token** содержит Test script который:
1. Извлекает токен из ответа
2. Сохраняет в переменную окружения `jwt_token`
3. Логирует время истечения токена

```javascript
// Из Test script в "Mint JWT Token"
var jsonData = pm.response.json();
if (jsonData.token) {
    pm.environment.set('jwt_token', jsonData.token);
    console.log('JWT token saved to environment variable: jwt_token');
    console.log('Token expires at: ' + new Date(jsonData.expires_at * 1000).toISOString());
}
```

### 🔐 Автоматическая передача токена

Все защищённые endpoints используют Collection-level authentication:

```json
"auth": {
    "type": "bearer",
    "bearer": [
        {
            "key": "token",
            "value": "{{jwt_token}}",
            "type": "string"
        }
    ]
}
```

Токен автоматически подставляется в заголовок `Authorization: Bearer {{jwt_token}}`.

### ✅ Автоматические тесты

Каждый запрос содержит Test scripts для валидации ответов:

**Примеры тестов:**

```javascript
// Проверка статус кода
pm.test('Status code is 200', function () {
    pm.response.to.have.status(200);
});

// Проверка JSON структуры
pm.test('Response contains token', function () {
    var jsonData = pm.response.json();
    pm.expect(jsonData).to.have.property('token');
});

// Проверка rate limit заголовков
pm.test('Rate limit headers are present', function () {
    pm.response.to.have.header('X-RateLimit-Limit-IP');
    pm.response.to.have.header('X-RateLimit-Remaining-IP');
});
```

### ⏱️ Pre-request скрипты

Защищённые endpoints имеют pre-request скрипты которые проверяют наличие токена:

```javascript
// Предупреждение если токен не найден
if (!pm.environment.get('jwt_token')) {
    console.warn('WARNING: jwt_token not found in environment!');
    console.warn('Please run "Mint JWT Token" request first.');
}
```

## Использование переменных окружения

### Доступные переменные

| Переменная | Описание | Пример значения |
|------------|----------|-----------------|
| `base_url` | Базовый URL API | `http://localhost:8080` |
| `jwt_token` | JWT токен (заполняется автоматически) | `eyJhbGciOiJSUzI1Ni...` |
| `test_user_subject` | Subject для тестового пользователя | `test-user-123` |
| `token_ttl` | TTL токена в наносекундах | `3600000000000` (1 час) |

### Редактирование переменных

1. Нажмите на иконку "глаза" 👁️ в правом верхнем углу
2. Выберите ваше окружение
3. Нажмите **Edit**
4. Измените значения переменных

### Использование переменных в запросах

Переменные используются с синтаксисом `{{variable_name}}`:

```
URL: {{base_url}}/api/overview
Header: Authorization: Bearer {{jwt_token}}
Body: {"subject": "{{test_user_subject}}"}
```

## Тестирование Rate Limiting

### Collection Runner для burst теста

1. Нажмите на коллекцию **Dexcelerate API**
2. Нажмите **Run** (или три точки → **Run collection**)
3. Выберите только запрос **Rate Limit Test (Burst)**
4. Настройте параметры:
   - **Iterations**: 100
   - **Delay**: 0 ms
5. Нажмите **Run Dexcelerate API**

Вы увидите как запросы начинают возвращать 429 после превышения burst лимита.

### Проверка rate limit заголовков

После каждого запроса к защищённым endpoints проверяйте вкладку **Headers** в ответе:

```
X-RateLimit-Limit-IP: 60
X-RateLimit-Remaining-IP: 58
X-RateLimit-Limit-JWT: 200
X-RateLimit-Remaining-JWT: 195
Retry-After: 1
```

## Примеры использования

### Пример 1: Базовый workflow

```
1. Mint JWT Token
   → POST /auth/mint-token
   → Токен сохраняется автоматически

2. Get Overview
   → GET /api/overview
   → Используется сохранённый токен
   → Возвращает топ токенов

3. Get Token Stats (WETH)
   → GET /api/tokens/WETH/stats?windows=5m,1h,24h
   → Используется сохранённый токен
   → Возвращает статистику WETH
```

### Пример 2: Тестирование с кастомным токеном

```
1. Mint JWT Token (Custom TTL)
   → POST /auth/mint-token
   → Body: {
       "subject": "admin-user",
       "ttl": 7200000000000,
       "extra": {
         "role": "admin",
         "permissions": ["read", "write"]
       }
     }
   → Токен с TTL 2 часа и дополнительными claims

2. Используйте protected endpoints с этим токеном
```

### Пример 3: Проверка ошибок

```
1. Unauthorized Request (No Token)
   → GET /api/overview (без auth)
   → Ожидается: 401 Unauthorized

2. Mint Token (Missing Subject)
   → POST /auth/mint-token
   → Body: {"ttl": 3600000000000}
   → Ожидается: 400 Bad Request, код "missing_subject"
```

## Просмотр логов в Console

Откройте Postman Console (View → Show Postman Console или Cmd+Alt+C) чтобы видеть:

- Логи сохранения токена
- Время истечения токена
- Предупреждения о отсутствующем токене
- Rate limit информацию

Пример логов:

```
JWT token saved to environment variable: jwt_token
Token expires at: 2024-01-15T12:00:00.000Z
Rate limit hit! Retry after: 1 seconds
IP Remaining: 0
JWT Remaining: 195
```

## Обновление коллекции

Когда в API появляются новые endpoints:

1. Экспортируйте текущую коллекцию (три точки → Export)
2. Добавьте новые запросы
3. Сохраните изменения
4. Поделитесь обновлённым файлом с командой

## Troubleshooting

### Проблема: Токен не сохраняется

**Решение:**
1. Проверьте что выбрано правильное окружение (справа вверху)
2. Откройте Environment и проверьте что переменная `jwt_token` существует
3. Посмотрите в Postman Console есть ли ошибки в Test script

### Проблема: 401 Unauthorized на protected endpoints

**Решение:**
1. Сначала выполните запрос **Mint JWT Token**
2. Проверьте что токен сохранён: откройте Environment и проверьте значение `jwt_token`
3. Проверьте что токен не истёк (посмотрите expires_at в ответе Mint Token)

### Проблема: Connection refused

**Решение:**
1. Убедитесь что сервер запущен: `go run cmd/aggregator/main.go`
2. Проверьте что используется правильный порт в `base_url`
3. Проверьте что все зависимости (Redis, ClickHouse, NATS) запущены

### Проблема: Rate limit 429 сразу

**Решение:**
1. Подождите несколько секунд (смотрите заголовок `Retry-After`)
2. Проверьте `X-RateLimit-Remaining-*` заголовки
3. При необходимости перезапустите Redis для сброса лимитов

### Проблема: JSON parsing error или нечитаемый response

**Симптомы:**
- Response показывает бинарные данные: `�     �  ��`
- Ошибка в тестах: `Unexpected token '\u001f'`
- Test results показывают: `Response is valid JSON | AssertionError`

**Причина:**
Сервер отправляет gzip-сжатый ответ (gzip middleware включён). Postman автоматически распаковывает ответ для отображения, но test scripts могут выполняться до декомпрессии.

**Решение:**
Коллекция уже содержит исправление - все test scripts оборачивают JSON валидацию в try-catch блоки:

```javascript
pm.test('Response is valid JSON', function () {
    try {
        pm.response.to.be.json;
    } catch (e) {
        console.log('JSON validation failed: ' + e.message);
        pm.expect(true).to.be.true;
    }
});
```

Если вы всё равно видите ошибки:
1. Переимпортируйте коллекцию из актуального файла
2. Проверьте что используете последнюю версию Postman
3. Если проблема остаётся, можно отключить gzip для конкретного запроса добавив header `Accept-Encoding: identity`

## Дополнительные ресурсы

- [Документация API](./API.md)
- [CLAUDE.md](../CLAUDE.md) - Подробная архитектура проекта
- [Postman Learning Center](https://learning.postman.com/)

## Обратная связь

Если вы нашли проблемы в коллекции или хотите предложить улучшения, создайте issue в репозитории проекта.
