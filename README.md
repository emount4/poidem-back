# poidem-back

Бэкенд веб-сайта на Go, Gin и PostgreSQL с чистой архитектурой.

## Запуск

Нужны запущенный Docker с Compose и GNU Make.

```sh
make init
make up
```

`make init` создаёт `.env` из `.env.example`. При необходимости измените настройки перед запуском.

Приложение доступно на `http://localhost:8080`.

Без Make: скопируйте `.env.example` в `.env` и выполните:

```sh
docker compose up -d --build --wait
```
