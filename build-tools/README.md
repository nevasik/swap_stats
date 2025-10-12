### Имитирует "производителя", начнет шить JSON-события в raw-swaps, с ключом event_id = chain:tx:logIndex - под консьюмера/дедуп/ClickHouse схему.

### как запустить
```bash
 go run ./build-tools/loadgen.go \
-brokers localhost:9092 \
-topic raw-swaps \
-rps 1000 \
-duration 60s \
-tokens USDC,ETH,WBTC,DAI
```


### Как проверить, что сообщения приходят
#### через rpk
``` bash
 rpk topic consume raw-swaps -b localhost:9092 -n 5
```

#### или в ClickHouse после вашего батчера
``` bash
 clickhouse-client -h localhost --query "SELECT count() FROM swaps.raw_swaps"
```

#### Заметки
1. ключ сообщения = event_id → стабильные партиции, как в проде.
2. генератор иногда делает “поздние” события (на 0–120 с назад), чтобы проверить вашу grace.
3. формат JSON совпадает с нашим контрактом, так что весь v0 → v1 стек можно тестировать end-to-end.
4. если нужно, добавлю флаги для распределения токенов, вероятности removed, профиля нагрузки (пульсации/бурсты).
