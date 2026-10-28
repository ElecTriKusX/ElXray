# Результаты текущей проверки

Среда: Linux amd64, Go 1.27.0. Это локальные результаты рабочего дерева runtime policy. CI для Windows/macOS добавлен, но его удалённый результат этим документом не подтверждается.

| Проверка | Результат |
|---|---|
| go test -race: common/subscriptionrate, app/dispatcher, app/policy, app/policy/command, common/buf, proxy, infra/conf, testing/subscriptionrate | PASS; для полного infra/conf использованы локальные geoip/geosite assets |
| go test -race ./transport/internet/tls -run '^TestCertificate' | PASS, включая конкурентную перезагрузку сертификата |
| go vet: изменённые целевые пакеты, CLI API и transport/internet/tls | PASS; полный infra/conf vet не заявлен |
| FuzzRateChunksPreservePayload, 15 с, 2 workers | PASS, 3565 исполнений; короткий smoke |
| Python unittest | PASS, 7 тестов |
| vformat / git diff --check | PASS |
| Сборка Linux amd64 | PASS |
| Кросс-сборка Windows amd64, Linux arm64, macOS arm64 | PASS; запуск на этих ОС этим не подтверждён |
| run -test node.example.json и node.tun.example.json | PASS |
| local_smoke.py, реальный бинарник + HTTP + SOCKS/VLESS | PASS: обе стороны, shared rate, API/retry/conflict/disable/unlimited, cleanup |

В последнем коротком процессе smoke, 2 MiB на поток: при начальной политике 10 down / 5 up получено 8,898 down / 4,812 up Мбит/с для двух соединений каждого направления. После runtime-изменения на 5 down / 2,5 up — 4,852 / 2,452 Мбит/с для одного соединения направления. Направления измеряются по собственному общему окну завершения; сумма средних отдельных потоков не используется. Это функциональный smoke с широким допуском, не результат трёх 30-секундных окон строгой скоростной приёмки.

Обычный и race сетевые прогоны включают VLESS TCP, TCP mux, UDP, UDP mux, XUDP; тест Vision использует TLS/REALITY и настоящий внутренний TLS 1.3, затем включает лимит на уже установленных потоках. Он проверяет данный путь, а не все возможные комбинации transport/flow/mux.

Полный transport/internet/tls suite здесь не прошёл: исходный ECH тест обращается к внешним DNS/Cloudflare и завершается при недоступной сети. Не смешивать это с успешным локальным certificate regression. Ранее на новом Vision race-тесте воспроизведена гонка чтения live TLS certificate против фонового reload; после исправления selected race-прогоны проходят.

Физические Windows/macOS устройства, пара устройств в LAN, ОС TUN, WebRTC провайдеры, длительная нагрузка и CPU/RSS/FD ещё не проверены. Методика: [ACCEPTANCE_TESTS.md](ACCEPTANCE_TESTS.md). Код проверки устройств/подлинности приложения и UI панели ещё не реализован; наличие готовых тестовых сценариев не означает наличие этих компонентов.
