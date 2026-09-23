# HTTP API

Публичные маршруты имеют префикс `/api/v1`. `GET /api/v1` возвращает
`{"version":"v1"}`. Служебные `/health` и `/ready` находятся вне API-версий.

Все версии подключаются в `routes.go`. Общие HTTP middleware и служебные маршруты
находятся в `internal/api`, а бизнес-обработчики — рядом со своим модулем, например
`internal/catalog/httpv1`. Бизнес-логика не зависит от Gin.

Чтобы добавить версию:

1. Создайте пакет `internal/api/v2` с функцией
   `RegisterRoutes(routes *gin.RouterGroup)` по примеру `v1`.
2. Подключите его в `routes.go`: `v2.RegisterRoutes(api.Group("/v2"), dependencies)`.
3. Внутри версии регистрируйте относительные пути: `routes.GET("/events", handler)`
   даст `/api/v2/events`.

Версии работают одновременно. Общие middleware (Request ID, логи, recovery)
подключены до групп и действуют на все версии. Middleware конкретной версии
добавляются через `routes.Use(...)` перед регистрацией её обработчиков.
