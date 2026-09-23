# Ответы на вопросы фронтендера: Poydem API v1

Этот документ закрывает вопросы из раздела «Что нужно согласовать» в
`frontend-handoff.md` и фиксирует решения для MVP. Исходный OpenAPI
после этого необходимо синхронизировать с данными решениями.

## 1. Регистр enum

В JSON API все значения ролей, статусов и типов передаются **в нижнем
регистре**.

Примеры:

``` text
user
admin

pending
active
rejected
blocked
completed

open
request

solo
company

approved
cancelled
resolved
```

`error.code` остаётся в верхнем регистре `UPPER_SNAKE_CASE`:

``` text
COMPANY_FULL
USER_BANNED
EVENT_NOT_AVAILABLE
```

Таким образом, frontend может строить типы непосредственно по OpenAPI и
не выполнять преобразование регистра.

## 2. Вход, callback и окружения

Фиксируем следующий OAuth flow:

1.  Frontend переводит пользователя на `GET /api/v1/auth/{provider}`.
2.  Backend выполняет OAuth flow с Google / Telegram / VK.
3.  После успешного callback backend создаёт refresh-сессию и
    устанавливает refresh token в `HttpOnly` cookie.
4.  Backend выполняет `302` на frontend.
5.  После открытия frontend вызывает `POST /api/v1/auth/refresh` с
    `credentials: include`.
6.  Backend возвращает `accessToken`.
7.  Frontend вызывает `GET /api/v1/auth/me`.

Access token **никогда не передаётся через URL**.

Маршруты frontend:

``` text
успешный вход: /auth/callback
ошибка входа:  /auth/error
```

Конкретные домены dev/prod не фиксируем в API-контракте: они задаются
переменными окружения.

Backend должен иметь настройки:

``` text
FRONTEND_URL
API_URL
OAUTH_CALLBACK_URL
```

Для production предполагается HTTPS.

Если frontend и API находятся на разных origin:

``` text
frontend: credentials: include
backend: Access-Control-Allow-Credentials: true
backend: Access-Control-Allow-Origin: конкретный FRONTEND_URL
```

`*` вместе с credentials не используется.

Cookie в production:

``` text
HttpOnly
Secure
SameSite=Lax
```

Если архитектура потребует действительно cross-site cookie,
`SameSite=None; Secure` настраивается отдельно.

Для локальной разработки допускается отдельная dev-конфигурация cookie
без `Secure`, если приложение работает по HTTP.

## 3. Refresh, logout и несколько запросов

На frontend должен существовать **один выполняющийся refresh request**.
Если несколько запросов одновременно получили `401`, остальные ожидают
результат уже запущенного refresh, а не запускают собственный.

После успешного refresh исходные запросы можно повторить один раз.

Бесконечный цикл:

``` text
401 -> refresh -> retry -> 401 -> refresh...
```

запрещён.

### Logout

`POST /auth/logout` отзывает текущую refresh-сессию и очищает refresh
cookie.

Текущий access JWT технически может оставаться криптографически валидным
до окончания своих 15 минут, поэтому frontend после logout обязан
немедленно удалить access token из памяти и очистить пользовательское
состояние.

Backend не обязан хранить blacklist каждого access JWT для обычного
logout.

Для `ban` все refresh-сессии пользователя отзываются. Защищённые
endpoints дополнительно проверяют `user.status`, поэтому заблокированный
пользователь не получает доступ даже с ещё не истёкшим access JWT.

### Потеря ответа refresh

Refresh token ротируется. Backend должен предусмотреть короткое окно
повторного использования/идемпотентности ротации либо корректную
обработку гонки, чтобы потеря сетевого ответа не приводила к
бесконечному refresh.

Для MVP frontend при окончательном провале refresh очищает локальную
авторизацию и переводит пользователя на вход.

Синхронизация нескольких вкладок может выполняться через
`BroadcastChannel` или событие `storage`; конкретная реализация остаётся
на frontend.

## 4. Onboarding

До завершения onboarding пользователь считается авторизованным, но
профиль --- незаполненным.

Разрешены:

-   `/auth/me`;
-   получение справочников;
-   чтение публичного каталога и карточек событий;
-   `GET/PATCH /users/me`;
-   загрузка аватара;
-   logout.

Действия, создающие пользовательский контент или участие, требуют
завершённого профиля:

-   создание события;
-   создание компании;
-   вступление в компанию;
-   подача заявки;
-   solo-участие;
-   создание жалобы.

Для таких действий backend возвращает:

``` text
403 PROFILE_INCOMPLETE
```

После того как профиль заполнен, `cityId` нельзя очистить через `null`.

В `User` добавляем явное поле:

``` json
{
  "isProfileComplete": true
}
```

Backend является источником истины для этого признака. Для MVP:

``` text
isProfileComplete = firstName заполнен AND city_id != null
```

Frontend не должен самостоятельно дублировать это бизнес-правило.

## 5. Время и уже начавшиеся события

Событие с:

``` text
startsAt <= now < endsAt
```

считается **ongoing**, а не past.

Оно:

-   остаётся доступным по прямой ссылке;
-   отображается в публичном каталоге до `endsAt`;
-   относится к `upcoming` в `/users/me/events` для простоты MVP;
-   допускает вступление/solo-участие, пока событие не завершено и
    компания принимает участников.

Событие становится `completed`, когда:

``` text
endsAt <= now
```

Если `endsAt` отсутствует:

``` text
startsAt <= now -> completed
```

Поэтому форма создания события должна явно предупреждать, что событие
без `endsAt` будет считаться завершённым сразу после времени начала.
Желательно сделать `endsAt` обязательным на frontend, хотя API может
пока сохранять nullable.

### dateFrom/dateTo

`dateFrom` и `dateTo` трактуются как календарные даты в часовом поясе,
переданном frontend.

Чтобы не вводить скрытую зависимость от timezone сервера, в итоговом API
лучше передавать полноценные ISO 8601 границы:

``` text
2026-09-23T00:00:00+03:00
2026-09-23T23:59:59+03:00
```

Если контракт сохраняет `format: date`, backend трактует дату в UTC.
Предпочтительный вариант перед реализацией --- заменить фильтры на
`date-time`.

Все timestamps в ответах содержат timezone/offset и хранятся в БД как
`TIMESTAMPTZ`.

## 6. «Мои события», компании и история

В `/users/me/events` добавляем:

``` text
relation:
creator
participant
creator_and_participant
```

Событие не дублируется, даже если пользователь одновременно creator и
participant.

### Мои компании

По умолчанию `/users/me/companies` возвращает только актуальные
компании, которые не удалены.

Заблокированная компания, в которой пользователь является участником или
владельцем, может отображаться в личном разделе с:

``` text
status = blocked
```

и без доступных действий вступления/редактирования.

Soft-deleted компания в обычном списке не возвращается.

### Мои заявки

`/users/me/applications` является историческим списком и возвращает
заявки всех статусов:

``` text
pending
approved
rejected
cancelled
```

Поддерживается пагинация.

### Ссылка на скрытую компанию

Для обычного пользователя:

-   soft-deleted компания -\> `404 COMPANY_NOT_FOUND`;
-   blocked компания для постороннего пользователя -\>
    `404 COMPANY_NOT_FOUND`;
-   blocked компания для её текущего участника/владельца -\> `200` с
    `status=blocked`;
-   admin может получать её через admin endpoints.

Так мы не раскрываем существование скрытого объекта посторонним
пользователям.

## 7. Действия владельца и ожидающие заявки

### Close

После `close` новые заявки и прямое вступление запрещены.

Ранее созданные `pending` заявки **можно одобрять или отклонять**, пока:

-   событие не завершено;
-   компания не заблокирована и не удалена;
-   в компании есть место.

Это позволяет владельцу сначала закрыть новый набор, а затем разобрать
уже полученные заявки.

### Block компании

При административном `block` все `pending` заявки автоматически
переводятся в:

``` text
cancelled
```

с системной причиной `COMPANY_BLOCKED`.

Одобрять их после block нельзя.

### Завершение события

После перехода события в `completed` все оставшиеся `pending` заявки
компаний этого события переводятся в:

``` text
cancelled
```

с причиной `EVENT_COMPLETED`.

Новые join/application/solo запрещены.

### Точные ошибки

Попытка владельца выйти через endpoint участника:

``` text
409 OWNER_CANNOT_LEAVE
```

Попытка вступить в другую компанию того же события:

``` text
409 ALREADY_IN_EVENT_COMPANY
```

Если пользователь уже находится именно в целевой компании:

``` text
409 ALREADY_COMPANY_MEMBER
```

Если компания заполнена:

``` text
409 COMPANY_FULL
```

Если набор закрыт:

``` text
409 COMPANY_CLOSED
```

Если компания заблокирована:

``` text
404 COMPANY_NOT_FOUND
```

для постороннего пользователя.

## 8. Списки, пустые страницы, validation details, аватары и dashboard

### Какие endpoints пагинируются

Пагинация обязательна для:

``` text
GET /users/me/events
GET /users/me/companies
GET /users/me/applications

GET /events
GET /events/{eventId}/participants
GET /events/{eventId}/companies

GET /companies/{companyId}/members
GET /companies/{companyId}/applications

GET /admin/users
GET /admin/events
GET /admin/companies
GET /admin/reports
```

Справочники остаются обычными массивами:

``` text
GET /cities
GET /interests
GET /event-categories
```

### Пустой список

Для пустого результата:

``` json
{
  "items": [],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 0,
    "totalPages": 0
  }
}
```

`totalPages = 0` подтверждаем.

Если запрошена страница больше последней, API не возвращает ошибку.
Возвращается пустой `items` с фактическим `total` и `totalPages`.

### Ошибки полей

Для `VALIDATION_ERROR` фиксируем формат `details.fields`:

``` json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Некорректные данные",
    "details": {
      "fields": {
        "firstName": ["Обязательное поле"],
        "cityId": ["Город не найден"]
      }
    }
  }
}
```

Frontend не должен парсить `message` для определения поля.

### Аватар

Максимальный размер:

``` text
5 * 1024 * 1024 = 5 242 880 bytes
```

Допустимые MIME:

``` text
image/jpeg
image/png
image/webp
```

Рекомендуемые бизнес-коды:

``` text
AVATAR_TOO_LARGE
UNSUPPORTED_AVATAR_TYPE
AVATAR_UPLOAD_FAILED
```

HTTP:

``` text
413 AVATAR_TOO_LARGE
415 UNSUPPORTED_AVATAR_TYPE
500/503 AVATAR_UPLOAD_FAILED
```

При любой ошибке предыдущий аватар остаётся без изменений.

### Admin dashboard

`newRegistrations` считается за **последние 30 календарных дней**,
включая текущий день.

Чтобы смысл поля был очевиден, предпочтительно переименовать его в
контракте:

``` text
newRegistrations30d
```

Если имя `newRegistrations` сохраняется, его 30-дневная семантика должна
быть явно описана в OpenAPI.

## Итог для синхронизации OpenAPI

Перед генерацией frontend-типов необходимо внести в
`poydem-openapi.yaml` следующие изменения:

1.  Все enum в JSON привести к lower-case.
2.  Добавить `isProfileComplete` в `User`.
3.  Добавить `PROFILE_INCOMPLETE`.
4.  Зафиксировать OAuth redirect + refresh flow без токенов в URL.
5.  Описать единое поведение refresh/logout.
6.  Добавить `relation` в элементы `/users/me/events`.
7.  Унифицировать пагинацию перечисленных endpoints.
8.  Подтвердить `totalPages = 0` для пустого списка.
9.  Добавить `ongoing`-семантику события.
10. Уточнить фильтры времени и timezone.
11. Зафиксировать историю заявок.
12. Зафиксировать видимость blocked/deleted компаний.
13. Добавить `OWNER_CANNOT_LEAVE` и `ALREADY_IN_EVENT_COMPANY`.
14. Зафиксировать судьбу pending-заявок при close/block/completed.
15. Добавить `details.fields` для validation errors.
16. Зафиксировать лимит аватара 5 242 880 bytes и MIME-типы.
17. Зафиксировать период `newRegistrations` = 30 дней.

После этих изменений OpenAPI v1 используется как единый источник типов и
моков для frontend и backend.

