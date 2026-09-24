# Контракт frontend ↔ backend

Единый машиночитаемый контракт Poydem API v1 — [`api/openapi.yaml`](../api/openapi.yaml).
Он содержит все 53 операции, стабильные `operationId`, схемы запросов и ответов,
security-схемы и точный mapping `HTTP status → error.code` в `x-error-codes`.

Frontend генерирует типы и клиент только из этого файла. Документы
[`frontend-handoff-answers.md`](frontend-handoff-answers.md) и
[`backend-requirements-poydem.md`](backend-requirements-poydem.md) объясняют
продуктовые решения, но при расхождении приоритет имеет OpenAPI.

## Правила интеграции

- JSON enum передаются в lower-case. Стабильные коды ошибок и системные причины —
  в `UPPER_SNAKE_CASE`.
- Access JWT хранится frontend в памяти и передаётся как `Authorization: Bearer ...`.
- Refresh token доступен только браузеру в HttpOnly cookie. Для refresh/logout
  запросы отправляются с `credentials: include`.
- OAuth callback не передаёт токены через URL. После редиректа на `/auth/callback`
  frontend вызывает `refreshAccessToken`, затем `getCurrentUser`.
- UI ветвится по `error.code`; `error.message` предназначен для отображения.
  Для `VALIDATION_ERROR` ошибки полей лежат в `error.details.fields`.
- `401` допускает один общий refresh и один повтор исходного запроса. `USER_BANNED`
  не запускает refresh-loop.
- Nullable-ключи ответа присутствуют в JSON и содержат значение или `null`.
  Пустые коллекции возвращаются как `[]`.
- PATCH: отсутствующий ключ сохраняет значение, `null` очищает nullable-поле,
  `interestIds: []` очищает интересы.
- Все пагинированные ответы имеют `{items, pagination}`. Для пустого результата
  `items=[]`, `total=0`, `totalPages=0`.
- `X-Request-ID` из ответа сохраняется в диагностике ошибок.

## OAuth URLs

- `GET /api/v1/auth/{provider}` — начало входа.
- Успех callback перенаправляет на `${FRONTEND_URL}/auth/callback`.
- Ошибка callback перенаправляет на `${FRONTEND_URL}/auth/error`.

Поддерживаемые значения `provider`: `google`, `telegram`, `vk`. Backend подключает
их последовательно; ещё не настроенный провайдер возвращает
`503 OAUTH_PROVIDER_UNAVAILABLE`.
