# Чем будет отличаться PROD-config от текущего dev

## Ingest (Redpanda/Kafka)

* `brokers`: список из **нескольких** брокеров (DNS/балансер или прямые адреса).
* **TLS/SASL**: включить шифрование и аутентификацию (mTLS или SASL SCRAM).
  В конфиг добавятся поля вида:

  ```yaml
  ingest:
    tls:
      enabled: true
      ca_file: "/etc/ssl/ca.pem"
      cert_file: "/etc/ssl/app.crt"
      key_file: "/etc/ssl/app.key"
    sasl:
      enabled: true
      mechanism: "SCRAM-SHA-256"
      username: "client"
      password: "****"
  ```
* `max_inflight`: поднять (например, 256–1024) после замера CPU/RAM.
* Тайминги: часто оставляют `session_timeout=30s`, `rebalance_timeout=120s` в больших кластерах.

> Кол-во партиций и retention на топике — это уже не `config.yaml` приложения, а политика кластера. В проде: **репликация=3**, **партиций** — по целевой пропускной способности и количеству инстансов.

## Redis (дедуп/снапшоты/лимиты)

* Пароль/TLS:

  ```yaml
  stores:
    redis:
      addr: "redis:6379"
      username: "default"
      password: "****"
      tls: true
      db: 2
  ```
* В проде часто используют **Redis Sentinel** или **Cluster** (другие адреса/URI).

## ClickHouse (хранилище)

* Реальный DSN (а не localhost) + учётные данные / TLS.
* Политика хранения (S3) и TTL дольше/строже. В dev 90d ок; в проде — по требованиям.

## NATS (фан-аут)

* Включить **аутентификацию** (user/pass, nkey или JWT), возможен кластер из 3 нод, квоты JetStream (хранилище, ретеншн).

## JWT (RS256)

* Поменять пути к ключам и секреты на продовые, добавить ротацию ключей (kid/ключевой набор):

  ```yaml
  security:
    jwt:
      enabled: true
      alg: "RS256"
      public_key_path: "/etc/keys/jwt.pub"
      private_key_path: "/etc/keys/jwt.pem"
      audience: "swap-stats-prod"
      issuer: "https://auth.example.com"
      leeway_sec: 30
  ```
* Часто в проде **запрещают** доступ без JWT (уже `enabled: true`).

## Rate limit

* Числа меняются по результатам нагрузочных тестов:
  больше клиентов → выше `refill_per_sec`, но и аккуратнее с `burst`, чтобы не схлопнуться пиками.

## Снапшоты/Grace

* `snapshot_interval`: иногда **увеличивают** до 10–30s, чтобы уменьшить нагрузку на Redis.
* `grace`: зависит от задержек источника. Если бывают поздние события, поднимут до 180–300s.

## API/Network

* `api.http.addr: ":8080"` останется, но в проде обычно встаёт перед приложением **reverse proxy** (nginx/Envoy) + mTLS, CORS/headers.

---

### Мини-пример чем будет отличаться от текущей конфигурации

```diff
 ingest:
-  brokers: ["redpanda:9092"]
+  brokers: ["kafka-1:9092","kafka-2:9092","kafka-3:9092"]
+  tls:
+    enabled: true
+    ca_file: "/etc/ssl/ca.pem"
+    cert_file: "/etc/ssl/app.crt"
+    key_file: "/etc/ssl/app.key"
+  sasl:
+    enabled: true
+    mechanism: "SCRAM-SHA-256"
+    username: "app"
+    password: "****"
-  max_inflight: 64
+  max_inflight: 256
-  rebalance_timeout: "60s"
+  rebalance_timeout: "120s"

 stores:
   redis:
-    addr: "redis:6379"
+    addr: "redis-sentinel:26379"
+    sentinel_master: "mymaster"
+    username: "default"
+    password: "****"
+    tls: true

 security:
   jwt:
-    public_key_path: "./secrets/jwt.pub"
-    private_key_path: "./secrets/jwt.pem"
+    public_key_path: "/etc/keys/prod-jwt.pub"
+    private_key_path: "/etc/keys/prod-jwt.pem"
+    audience: "swap-stats-prod"
+    issuer: "https://auth.example.com"

 rate_limit:
   by_jwt:
-    refill_per_sec: 50
-    burst: 200
+    refill_per_sec: 200
+    burst: 400
   by_ip:
-    refill_per_sec: 20
-    burst: 60
+    refill_per_sec: 50
+    burst: 150

 app:
-  snapshot_interval: "5s"
+  snapshot_interval: "15s"
-  grace: "120s"
+  grace: "180s"

 stores:
   clickhouse:
-    dsn: "tcp://clickhouse:9000?database=swaps&compress=true"
+    dsn: "tcp://ch-proxy:9000?database=swaps&secure=true&user=app&password=****"
```

> Это ориентир: точные значения подбирать отталкиваясь больше от НФТ и по замерам
