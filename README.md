# poidem-back

Бэкенд веб-сайта на Go, Gin, PostgreSQL и MinIO с чистой архитектурой.

## Запуск

Нужны запущенный Docker с Compose и GNU Make.

```sh
make init
make up
```

`make init` создаёт `.env` из `.env.example`. При необходимости измените настройки перед запуском.

Миграции БД применяются автоматически перед стартом приложения.

Приложение доступно на `http://localhost:8080`.
MinIO API доступен на `http://localhost:9000`, консоль — на
`http://localhost:9001`. Логин и пароль задаются через `MINIO_ROOT_USER` и
`MINIO_ROOT_PASSWORD`; аватары сохраняются в постоянном Docker volume.

Без Make: скопируйте `.env.example` в `.env` и выполните:

```sh
docker compose up -d --build --wait
```
