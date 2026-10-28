# Windows: локальный стенд и два устройства в домашней LAN

VPS не требуется. На ПК запускается серверная роль Xray, хотя файл называется node.example.json. Для проверки общего лимита TCP TUN не нужен. До появления собственного клиента TUN/устройства не считать проверенными.

## Почему команда curl могла вернуть HTTP 400

В Windows PowerShell 5.1 / legacy argument passing пустой аргумент `--noproxy ""` может потеряться до запуска curl.exe. Следующий `--socks5-hostname` тогда становится значением noproxy, а адрес SOCKS может интерпретироваться как отдельный HTTP URL. Несколько строк bytes=0 согласуются с этим. Это вероятная причина конкретного вывода, а не доказательство без проверки аргументов на пользовательском ПК.

На Linux воспроизведена передача команды с уже потерянным пустым аргументом и проверена исправленная форма. Поддерживаемая альтернатива для curl без пустых аргументов:

```powershell
curl.exe --fail --silent --show-error --max-time 180 --noproxy never-bypass.invalid --socks5-hostname 127.0.0.1:1080 "http://127.0.0.1:18080/download?bytes=67108864" -o NUL -w "bytes=%{size_download} seconds=%{time_total} speed_Bps=%{speed_download}\n"
```

never-bypass.invalid — намеренно не совпадающее с 127.0.0.1 значение исключения. DNS-запрос к нему не выполняется. Не заменять его на `*`: это отключило бы использование прокси. Синтаксис `--noproxy=` curl не поддерживает.

Если старые PowerShell Jobs остались незавершёнными, остановить только сохранённые задания этого теста:

```powershell
$downloadJobs | Stop-Job
$downloadJobs | Receive-Job
$downloadJobs | Remove-Job
```

По одному сообщению Remove-Job нельзя определить, почему задание осталось активным. Не останавливать все задания/процессы curl системы.

## Автоматическая приёмка и runtime

Актуальный полный сценарий: [ACCEPTANCE_TESTS.md](ACCEPTANCE_TESTS.md). После сборки можно выполнить `python3 testing/subscriptionrate/local_smoke.py --xray .\xray-lab.exe`; прежние процессы на 1080/18080/24443/10085 перед этим остановить вручную. Скрипт сам проверяет скорости/API и убирает свои процессы.

## Быстрый путь: без curl и Start-Job

Оставьте запущенными HTTP fixture, серверный и клиентский Xray из предыдущей инструкции. Все три процесса работают на одном ПК; SSH не нужен.

В четвёртом терминале из корня репозитория:

```powershell
python3 testing/subscriptionrate/bench_client.py --connections 1 --bytes 1048576
python3 testing/subscriptionrate/bench_client.py --connections 1
python3 testing/subscriptionrate/bench_client.py --connections 2
python3 testing/subscriptionrate/bench_client.py --connections 2 --direction upload
```

Runner использует SOCKS5 напрямую через стандартную библиотеку Python. Переменные HTTP_PROXY/NO_PROXY не участвуют; прямого fallback нет. Параметры не требуют передачи пустых строк shell. Дополнительные Python-пакеты не нужны.

По умолчанию каждый поток передаёт 16 MiB. Для node.example.json ожидается aggregate_Mbps около 10 для download и около 5 для upload, если нет других активных передач этой подписки. JSON включает результаты потоков и суммарную скорость по общему wall-clock интервалу. Это проверка всей передачи, не синхронное 30–60-секундное окно. При двух потоках равные доли не гарантируются. На очень маленьком объёме начальный burst заметно искажает среднее.

Runner проверяет статус HTTP, количество и нулевое содержимое download; для upload — подтверждение полного приёма сервером. При отказе соединения завершается с ошибкой. Остановите клиентский Xray и выполните маленькую проверку: она должна завершиться ошибкой, затем снова запустите клиент.

## Два компьютера в одной сети

Если второе устройство — Windows/Linux/macOS с собранным Xray, используйте две копии лабораторного клиента. Для Android/iOS нужна отдельная инструкция с учётом доступного приложения и ОС; эти JSON не устанавливают мобильный VPN сами по себе.

На ПК A остаются HTTP fixture и серверный Xray. На обоих ПК работают локальные SOCKS-клиенты. Клиенты подключаются к LAN-IP ПК A, используя один тестовый UUID. HTTP fixture и API продолжают слушать исключительно 127.0.0.1 на ПК A.

1. Найдите LAN IPv4 ПК A через ipconfig. Используйте адрес активного домашнего интерфейса, например 192.168.1.20; не адрес VPN-адаптера и не 0.0.0.0.
2. Сохраните отдельные копии конфигов вне checkout, например в D:\XrayLab. В node.lan.json измените только listen тестового VLESS inbound с 127.0.0.1 на LAN-IP ПК A. Не меняйте api.listen, HTTP fixture и узкое правило выхода на 127.0.0.1:18080.
3. Сгенерируйте новый тестовый UUID: `[guid]::NewGuid().ToString()`. Укажите его в серверном clients[0].id и в обоих клиентских settings.id. Сохраните server-side email lab@example.
4. В client.lan.json обоих устройств укажите settings.address = LAN-IP ПК A, port = 24443. Локальный SOCKS остаётся 127.0.0.1:1080 на каждом устройстве.
5. Остановите прежний серверный процесс и запустите отдельный конфиг: `.\xray-lab.exe run -config D:\XrayLab\node.lan.json`. Аналогично перезапустите клиентские процессы с client.lan.json. На ПК A клиент тоже подключается по LAN-IP, потому что сервер теперь привязан к нему.
6. Для Windows Firewall при необходимости добавьте правило ниже из PowerShell администратора, с реальным IP. Оно действует только для Private-профиля и локальной подсети. HTTP/API порты не открывайте.

```powershell
$LabIp = "192.168.1.20" # заменить на LAN-IP ПК A
New-NetFirewallRule -DisplayName "Electrium Xray LAN lab" -Direction Inbound -Action Allow -Protocol TCP -LocalAddress $LabIp -LocalPort 24443 -RemoteAddress LocalSubnet -Profile Private -Program (Get-Item .\xray-lab.exe).FullName
```

Правило Private не сработает при Public-профиле сети. Проброс портов на роутере не требуется. Этот plaintext VLESS-стенд передаёт только синтетические данные в доверенной домашней LAN; реальные подписки и рабочие ключи в нём не использовать. Снаружи дома к нему подключаться не нужно.

На каждом устройстве запустите `python3 testing/subscriptionrate/bench_client.py --connections 1`. Во время пересекающихся передач общий лимит на ПК A должен остаться 10 Мбит/с. Не суммируйте средние скорости двух законченных запросов, если окна различались; для общего окна снимайте счётчики на ПК A, как в TESTING.md. Для upload повторите с `--direction upload`.

Если подключения не устанавливаются, проверьте TCP доступность на ПК B: `Test-NetConnection LAN_IP_ПК_A -Port 24443`. Затем проверяйте прокси/UUID. Изоляция Wi-Fi клиентов/гостевая сеть роутера может препятствовать LAN-соединению.

После теста остановите только лабораторные процессы. Удалите добавленное правило по точному имени:

```powershell
Get-NetFirewallRule -DisplayName "Electrium Xray LAN lab" | Remove-NetFirewallRule
```

Для TUN и расширенной матрицы используется ACCEPTANCE_TESTS.md. REALITY/Vision и UDP/XUDP/mux дополнительно покрыты автоматическими сетевыми Go-тестами. Успех этого TCP-стенда не засчитывает их автоматически.

## Что проверено при добавлении runner

На Linux запущены три реальных процесса: HTTP fixture, серверный и клиентский Xray. Runner выполнил по два параллельных download и upload по 65536 байт на соединение. Python unittest: 4 теста, успешно. Это функциональный smoke, а не длительный замер лимита. Windows PowerShell и физическая LAN в этой среде не запускались.
