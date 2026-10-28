# ElXray

ElXray — форк [XTLS/Xray-core](https://github.com/XTLS/Xray-core) с общими лимитами скорости подписки и локальным API управления ими без перезапуска соединений. Лицензия — [MPL-2.0](LICENSE); исходные уведомления об авторских правах сохранены.

## Возможности

- Одна подписка объединяет несколько серверных email в общий бюджет.
- Upload и download ограничиваются независимо внутри одного экземпляра ядра.
- Параллельные подключения и разные IP не умножают доступную полосу.
- Runtime API применяет новые скорости к существующим соединениям, включая переход из unlimited в limited.
- Ревизии политик защищают от устаревших обновлений; точный повтор принятого запроса безопасен.
- Блокировка подписки через `disabled`, запрет пользователей без назначенной политики через `requireSubscription`.
- Защита от обхода бюджета через raw socket splice, включая Vision; сохранение guard под обёрткой статистики.
- Исправлена гонка обновления TLS-сертификатов с выбором сертификата во время handshake.

Бюджет действует на один процесс/ноду. Учитываются байты диспетчера, а не сетевые заголовки, padding и retransmission. Равные доли между потоками не гарантируются. Эти функции не определяют количество физических устройств.

## Сборка

Нужна версия Go из [go.mod](go.mod) или совместимая более новая версия.

```powershell
go build -o elxray.exe ./main
.\elxray.exe version
.\elxray.exe run -config config.json
```

Linux/macOS:

```sh
go build -o elxray ./main
./elxray run -config config.json
```

Go module path `github.com/xtls/xray-core` и protobuf namespace сохранены для совместимости исходников и SDK. Название продукта и сборок — ElXray. При интеграции с менеджером процессов учитывайте ожидаемое им имя исполняемого файла.

## Настройка лимитов

Добавьте в существующий серверный конфиг, сохраняя остальные поля `policy`:

```json
{
  "policy": {
    "requireSubscription": true,
    "subscriptions": {
      "account-123": {
        "emails": ["account-123@example"],
        "uplinkBps": 625000,
        "downlinkBps": 1250000,
        "revision": 1,
        "disabled": false
      }
    }
  }
}
```

Это фрагмент, не полный серверный конфиг. Email должен совпадать с email аутентифицированного серверного пользователя; это не адрес, передаваемый клиентом в HTTP-заголовке. Регистр нормализуется, дубликаты email отклоняются.

`Bps` — **байты в секунду**. Мбит/с × 1 000 000 / 8. Пример задаёт 5 Мбит/с upload и 10 Мбит/с download со стороны клиента. Одновременно разрешены обе скорости. Чтобы получить 100 Мбит/с в каждом направлении, задайте оба значения `12500000`; общий бюджет up+down не используется.

`0` означает unlimited для направления, `disabled: true` запрещает передачу в обоих направлениях. `requireSubscription: true` блокирует аутентифицированных пользователей без привязки, включая пустой email. Без этого флага неизвестные email сохраняют upstream-поведение.

Начальный burst — 8192 байта, максимум направления — 2^40 байт/с. Обновление политики не выдаёт новый burst. UDP-датаграммы сохраняются целиком; pacing средней скорости не является жёстким packet policing.

## Runtime API

```json
"api": {
  "tag": "api",
  "listen": "127.0.0.1:10085",
  "services": ["StatsService", "SubscriptionService"]
}
```

API допускается только на явном loopback-адресе или защищённом Unix socket. Локальные процессы считаются доверенными; штатный Commander не проверяет пароль локального пользователя. Не публикуйте API в LAN/Интернет.

```powershell
.\elxray.exe api subscription-get --server=127.0.0.1:10085 --id=account-123
.\elxray.exe api subscription-set --server=127.0.0.1:10085 --id=account-123 --expected-revision=1 --revision=2 --uplink-bps=312500 --downlink-bps=625000 --disabled=false
```

Обе скорости и disabled обязательны. Новая revision равна expected-revision + 1. Устаревший запрос с другой политикой отклоняется; некорректное обновление сохраняет действующее состояние.

**Runtime-изменения находятся в памяти.** Управляющий процесс обязан сохранять политику и формировать стартовый JSON с актуальными скоростями, revision и disabled. Ядро не перезаписывает конфиг. Создание/изменение email-привязок требует изменения стартового конфига и рестарта. Disabled не является отзывом UUID и не гарантирует мгновенного закрытия каждого idle socket.

Подробности: [контракт API](docs/electrium/RUNTIME_API.md) и [семантика лимитов](docs/electrium/SUBSCRIPTION_RATE_LIMIT.md).

## Проверка

```powershell
go test -race ./common/subscriptionrate ./app/dispatcher ./app/policy ./app/policy/command ./common/buf ./proxy ./testing/subscriptionrate -count=1 -timeout 120s
go test -race ./infra/conf -run '^TestSubscription' -count=1 -timeout 90s
go test -race ./transport/internet/tls -run '^TestCertificate' -count=1 -timeout 30s
python3 -m unittest discover -s testing/subscriptionrate -p 'test_*.py' -v
python3 testing/subscriptionrate/local_smoke.py --xray .\elxray.exe
```

Для race detector нужен поддерживаемый C toolchain. Для полного `go test ./...` нужны `resources/geoip.dat` и `resources/geosite.dat`; часть upstream-тестов также обращается к внешней сети. CI скачивает assets с проверкой SHA-256, не полагаясь на ранее заполненный кэш и права запуска другого workflow. Windows 7 использует отдельный patched toolchain; такая сборка не означает проверки на реальной Windows 7.

Smoke сам запускает HTTP fixture и два процесса ядра на loopback с временным UUID, проверяет общий бюджет, одновременный upload/download, API retry/conflict/disable/unlimited и останавливает собственные процессы. Перед запуском освободите лабораторные порты 1080, 18080, 24443 и 10085.

Сетевые тесты покрывают VLESS TCP, UDP, UDP mux, XUDP, TCP mux и Vision поверх TLS/REALITY с локальным TLS target. Успех этой матрицы не является проверкой всех транспортных сочетаний или длительной нагрузки.

[Ручной стенд на одном ПК и двух устройствах](testing/subscriptionrate/README.md).
