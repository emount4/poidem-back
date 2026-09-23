# Уточнения для backend-разработки проекта «Пойдём!»

Документ фиксирует продуктовые и технические решения для реализации backend MVP и дополняет API-контракт и схему БД. Решения по интеграции подтверждены в
[ответах фронтенда](frontend-handoff-answers.md). Все enum в JSON передаются в
нижнем регистре; `error.code` — в `UPPER_SNAKE_CASE`.

## 1. Авторизация

Для MVP используются OAuth-провайдеры: Google, Telegram и VK. Рекомендуемый порядок реализации: Google → Telegram → VK. Первый успешный вход автоматически создаёт пользователя. Email/password в MVP не нужны. Если профиль не заполнен, frontend открывает onboarding.

Сессии: access token — JWT на 15 минут; refresh token — 30 дней; refresh передаётся
через HttpOnly cookie, в БД хранится его hash, используется ротация с коротким окном
повторного запроса. Logout отзывает текущую refresh-сессию и очищает cookie; ban
отзывает все сессии пользователя. Нужна таблица `sessions` / `refresh_sessions`.

OAuth callback устанавливает cookie и перенаправляет на `${FRONTEND_URL}/auth/callback`,
после чего frontend получает access token через `POST /auth/refresh`. Ошибка входа
ведёт на `${FRONTEND_URL}/auth/error`; токены в URL не передаются. Настройки
`FRONTEND_URL`, `API_URL` и `OAUTH_CALLBACK_URL` приходят из окружения. Production
cookie: HttpOnly, Secure, SameSite=Lax. Для разных origin разрешается только точный
FRONTEND_URL с credentials; cross-site режим требует SameSite=None, Secure и отдельной
CSRF-защиты. В локальном HTTP-окружении Secure настраивается отдельно.

## 2. Роли

Системные роли:

```text
user
admin
```

Организатор компании — не глобальная роль. Пользователь является организатором, если `company.owner_id == user.id`. Первый `admin` создаётся через seed/config/БД.

## 3. Профиль

Обязательные поля заполненного профиля: `firstName`, `cityId`. Необязательные:
`lastName`, `avatar`, `about`, `interests`. До завершения onboarding `city` может
быть `null`. Пустые коллекции возвращаются как `[]`, а не `null`. `User` содержит
вычисляемое backend-поле `isProfileComplete`; для MVP оно истинно, когда заполнен
firstName и `city_id != null`. После завершения onboarding город нельзя очистить.

Незаполненному пользователю разрешены auth/me, справочники, публичное чтение событий,
GET/PATCH users/me, аватар и logout. Создание событий/компаний/жалоб и любое участие
возвращают `403 PROFILE_INCOMPLETE`.

## 4. PATCH

Все PATCH-запросы частичные:

```text
поле отсутствует -> не менять
поле = null -> очистить, если nullable
поле содержит значение -> заменить
```

Для PATCH используются отдельные DTO/schema, а не Create DTO с обязательными полями.

## 5. Жизненный цикл события

Событие может создать авторизованный пользователь с заполненным профилем. Новое событие получает `pending`.

```text
pending -> active
pending -> rejected
active -> blocked
active -> completed
```

`completed` определяется системой при `endsAt <= now`, а при отсутствии `endsAt` —
при `startsAt <= now`. Событие с `startsAt <= now < endsAt` считается ongoing:
оно остаётся в каталоге, допускает участие и относится к upcoming в личном списке.
`rejected` и `blocked` не видны публично. Автор видит свои pending и rejected события.

## 6. Каталог событий

По умолчанию публичный каталог возвращает `active` события, которые ещё не завершены. Сортировка по умолчанию:

```text
startsAt ASC
id ASC
```

`popular` для MVP:

```text
participantsCount DESC
id ASC
```

Все timestamps API — ISO 8601 с offset. В PostgreSQL — `TIMESTAMPTZ`, хранение в UTC.
`dateFrom`/`dateTo` предпочтительно сделать `date-time`; если контракт сохраняет
`format: date`, backend трактует границы в UTC.

## 7. «Мои события»

`GET /users/me/events` возвращает события, в которых пользователь участвует, и события, которые он создал, без дублей. Каждый элемент содержит `relation`: `creator`, `participant`, `creator_and_participant`. Фильтры: `upcoming`, `past`; ongoing относится к upcoming.

## 8. Создание компании

Компания создаётся только для существующего доступного события. Создатель автоматически становится owner, добавляется в `company_members` и становится участником события. Owner учитывается в `maxMembers`: при `maxMembers = 6` это владелец + максимум 5 участников.

## 9. Выход владельца

Owner не может выполнить `DELETE /companies/:id/members/me`. Передачи ownership в MVP нет. Для прекращения существования компании owner удаляет компанию.

## 10. Одна компания на событие

Один пользователь одновременно может состоять только в одной компании одного события. В разных событиях он может состоять в разных компаниях. Ограничение проверяется backend и должно быть защищено от race condition.

## 11. SOLO → COMPANY

Если пользователь сначала выбрал самостоятельное участие, а затем вступил в компанию этого события, backend атомарно меняет `solo -> company`. Дополнительное подтверждение не требуется. В `event_participants` остаётся одна запись на `(event_id, user_id)`.

## 12. Выход из компании

Выход полностью отменяет участие пользователя в событии. В одной транзакции:

```text
company_members DELETE
event_participants DELETE
```

Автоматического `company -> solo` нет.

## 13. Исключение участника

При исключении пользователя owner'ом также удаляются `company_members` и `event_participants` в одной транзакции.

## 14. Открытая компания

При `joinType = open` пользователь вступает без заявки. Backend проверяет: компания `active`, набор открыт, есть место, пользователь не состоит в другой компании этого event, событие доступно, пользователь не banned. Изменения выполняются транзакционно.

## 15. Вступление по заявке

При `joinType = request` создаётся заявка. Статусы:

```text
pending
approved
rejected
cancelled
```

Одновременно допускается только одна `pending` заявка пользователя в конкретную компанию.

## 16. Повторная заявка

После `rejected` или `cancelled` пользователь может подать заявку снова. Старую запись не перезаписываем — создаём новую. `/applications/me` сначала возвращает текущую `pending`, а если её нет — последнюю заявку пользователя в эту компанию. `/users/me/applications` возвращает пагинированную историю всех статусов.

## 17. Одобрение заявки

Approve выполняется одной транзакцией:

```text
application -> approved
company_members INSERT
event_participants INSERT/UPDATE
```

Перед approve повторно проверяются свободное место, отсутствие пользователя в другой компании этого события и актуальность компании/события. При конкурентной попытке занять последнее место успешно проходит только одна операция.

## 18. Закрытие набора

`POST /companies/:id/close` не удаляет компанию. Участники остаются, но новые
join/application запрещены. Ранее созданные pending-заявки можно approve/reject,
пока событие не завершено, компания не заблокирована/удалена и есть место. Owner
может вызвать `/open`, если компания не заблокирована и событие актуально.

## 19. Удаление компании

Для MVP используется soft delete. В `companies` добавить:

```text
deleted_at TIMESTAMPTZ NULL
```

После удаления компания исчезает из публичного API, pending-заявки становятся cancelled,
исторические `company_members` можно сохранить, а участие пользователей этой компании
в событии отменяется. Восстановление через API не требуется. Операция транзакционная.

## 20. Ban пользователя

При бане: `users.status = banned`, все refresh-сессии отзываются. Каждый защищённый
endpoint проверяет актуальный статус, поэтому ещё действующий access JWT не даёт доступ.
Исторические данные не удаляются. `admin` может выполнить unban.

## 21. Block компании

При административной блокировке `company.status = blocked`. Компания скрыта от
посторонних, но доступна текущим участникам/owner со статусом blocked. Новые
join/application запрещены, существующие участники и история сохраняются, все pending
заявки становятся cancelled с причиной `COMPANY_BLOCKED`. При завершении события
оставшиеся pending-заявки становятся cancelled с причиной `EVENT_COMPLETED`.

## 22. Жалобы

Жаловаться можно на `user`, `event`, `company`. Backend проверяет существование объекта. Для MVP допускается несколько жалоб одного пользователя на один объект. Статусы: `pending`, `resolved`, `rejected`.

## 23. Аватар

Поддерживаемые MIME: `image/jpeg`, `image/png`, `image/webp`. Максимум 5 242 880 bytes. Backend проверяет фактический MIME/type. Ответ:

```json
{
  "avatarUrl": "https://..."
}
```

Старый аватар удаляется только после успешного сохранения нового. При любой ошибке
он остаётся без изменений. Коды: `413 AVATAR_TOO_LARGE`,
`415 UNSUPPORTED_AVATAR_TYPE`, `500/503 AVATAR_UPLOAD_FAILED`. Конкретное
файловое/S3-совместимое хранилище — техническое решение backend-разработчика.

## 24. Пагинация

Все растущие списочные endpoints используют единый формат:

```json
{
  "items": [],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 123,
    "totalPages": 7
  }
}
```

Значения: `page = 1`, `limit = 20`, `max limit = 100`. Для пустого списка
`items=[]`, `total=0`, `totalPages=0`. Страница выше последней возвращает пустой
`items` с фактическими total/totalPages, а не ошибку. Пагинируются мои события,
компании и заявки; каталог событий; участники и компании события; члены и заявки
компании; списки admin users/events/companies/reports. Справочники остаются массивами.

## 25. Формат ошибок

Единый формат:

```json
{
  "error": {
    "code": "COMPANY_FULL",
    "message": "В компании больше нет свободных мест",
    "details": {}
  }
}
```

Основные коды:

```text
VALIDATION_ERROR
UNAUTHORIZED
FORBIDDEN
NOT_FOUND
USER_BANNED
PROFILE_INCOMPLETE
EVENT_NOT_FOUND
EVENT_NOT_AVAILABLE
COMPANY_NOT_FOUND
COMPANY_FULL
COMPANY_CLOSED
COMPANY_BLOCKED
NOT_COMPANY_OWNER
ALREADY_COMPANY_MEMBER
ALREADY_IN_EVENT_COMPANY
OWNER_CANNOT_LEAVE
APPLICATION_ALREADY_EXISTS
APPLICATION_NOT_FOUND
APPLICATION_ALREADY_RESOLVED
ALREADY_EVENT_PARTICIPANT
AVATAR_TOO_LARGE
UNSUPPORTED_AVATAR_TYPE
AVATAR_UPLOAD_FAILED
```

HTTP: `400` — некорректные данные; `401` — нет/невалидна авторизация; `403` — действие запрещено; `404` — объект отсутствует/недоступен; `409` — конфликт состояния.
Для `VALIDATION_ERROR` ошибки полей передаются как
`details.fields: Record<string, string[]>`; frontend не разбирает текст message.

## 26. Транзакции

Обязательно транзакционно выполнять: создание компании + owner + участие; вступление; выход; исключение; approve заявки; удаление компании; `solo -> company`. Особое внимание — конкурентным запросам на последнее свободное место.

## 27. Дополнения к схеме БД

Добавить `sessions` / `refresh_sessions`:

```text
id
user_id
token_hash
expires_at
revoked_at
created_at
```

В `companies` добавить `deleted_at TIMESTAMPTZ NULL`.

Для `event_participants` зафиксировать:

```text
UNIQUE(event_id, user_id)
```

## 28. Admin dashboard

`newRegistrations30d` считается за последние 30 календарных дней, включая текущий.
Если в OpenAPI сохраняется имя `newRegistrations`, эта семантика явно указывается
в его описании.

## 29. Что синхронизировать в OpenAPI перед реализацией

1. Сделать PATCH-схемы частичными.
2. Уточнить nullable/required поля профиля, добавить `isProfileComplete`.
3. Привести enum в JSON к нижнему регистру.
4. Унифицировать согласованный перечень пагинируемых списков и `totalPages=0`.
5. Зафиксировать OAuth redirect/refresh flow, cookie и logout без токенов в URL.
6. Добавить relation в мои события и ongoing-семантику времени.
7. Зафиксировать session/refresh-token модель и повтор ротации.
8. Зафиксировать переходы статусов событий, компаний, заявок и жалоб.
9. Зафиксировать soft delete и видимость blocked/deleted компании.
10. Описать validation details, бизнес-ошибки и HTTP-коды.
11. Зафиксировать правила solo и участия через компанию.
12. Добавить ограничение одной компании на одно событие.
13. Зафиксировать поведение ban/block/delete и pending-заявок.
14. Зафиксировать лимиты и ошибки аватара.
15. Описать 30-дневную семантику dashboard.

После синхронизации этот документ и `poydem-openapi.yaml` считаются согласованной спецификацией MVP backend v1.
