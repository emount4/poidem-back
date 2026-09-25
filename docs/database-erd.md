# ERD базы данных Poydem

Диаграмма ниже точно отражает схему после миграций
[`000001_initial_schema.up.sql`](../migrations/000001_initial_schema.up.sql) и
[`000002_refresh_sessions.up.sql`](../migrations/000002_refresh_sessions.up.sql),
ограничения событий дополнены миграцией
[`000003_events_hardening.up.sql`](../migrations/000003_events_hardening.up.sql),
а ограничения компаний и участия — миграцией
[`000004_companies_hardening.up.sql`](../migrations/000004_companies_hardening.up.sql).
Это 13 таблиц текущей базы.

```mermaid
erDiagram
    CITIES {
        bigint id PK
        varchar name
        varchar slug UK
    }

    INTERESTS {
        bigint id PK
        varchar name
        varchar slug UK
    }

    EVENT_CATEGORIES {
        bigint id PK
        varchar name
        varchar slug UK
    }

    USERS {
        bigint id PK
        varchar first_name
        varchar last_name
        text avatar_url
        bigint city_id FK
        text about
        varchar role
        varchar status
        timestamptz created_at
        timestamptz updated_at
    }

    AUTH_ACCOUNTS {
        bigint id PK
        bigint user_id FK
        varchar provider UK
        varchar provider_user_id UK
        timestamptz created_at
    }

    SESSIONS {
        bigint id PK
        bigint user_id FK
        bytea token_hash UK
        bytea previous_token_hash UK
        timestamptz previous_valid_until
        timestamptz expires_at
        timestamptz revoked_at
        timestamptz created_at
        timestamptz updated_at
    }

    USER_INTERESTS {
        bigint user_id PK,FK
        bigint interest_id PK,FK
    }

    EVENTS {
        bigint id PK
        bigint creator_id FK
        bigint category_id FK
        bigint city_id FK
        varchar title
        text description
        text image_url
        timestamptz starts_at
        timestamptz ends_at
        varchar location_name
        varchar address
        varchar status
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    COMPANIES {
        bigint id PK
        bigint event_id FK
        bigint owner_id FK
        varchar name
        text description
        int max_members
        varchar join_type
        text rules
        varchar status
        timestamptz created_at
        timestamptz updated_at
    }

    COMPANY_MEMBERS {
        bigint company_id PK,FK
        bigint user_id PK,FK
        varchar role
        timestamptz joined_at
    }

    APPLICATIONS {
        bigint id PK
        bigint company_id FK
        bigint user_id FK
        text message
        varchar status
        timestamptz created_at
        timestamptz resolved_at
    }

    EVENT_PARTICIPANTS {
        bigint event_id PK,FK
        bigint user_id PK,FK
        varchar participation_type
        bigint company_id FK
        timestamptz joined_at
    }

    REPORTS {
        bigint id PK
        bigint author_id FK
        varchar target_type
        bigint target_id
        varchar reason
        text description
        varchar status
        bigint resolved_by FK
        timestamptz created_at
        timestamptz resolved_at
    }

    CITIES o|--o{ USERS : city_id
    USERS o|--o{ AUTH_ACCOUNTS : user_id
    USERS ||--o{ SESSIONS : user_id
    USERS ||--o{ USER_INTERESTS : user_id
    INTERESTS ||--o{ USER_INTERESTS : interest_id

    USERS o|--o{ EVENTS : creator_id
    EVENT_CATEGORIES o|--o{ EVENTS : category_id
    CITIES o|--o{ EVENTS : city_id

    EVENTS o|--o{ COMPANIES : event_id
    USERS o|--o{ COMPANIES : owner_id
    COMPANIES ||--o{ COMPANY_MEMBERS : company_id
    USERS ||--o{ COMPANY_MEMBERS : user_id

    COMPANIES o|--o{ APPLICATIONS : company_id
    USERS o|--o{ APPLICATIONS : user_id

    EVENTS ||--o{ EVENT_PARTICIPANTS : event_id
    USERS ||--o{ EVENT_PARTICIPANTS : user_id
    COMPANIES o|--o{ EVENT_PARTICIPANTS : company_id

    USERS o|--o{ REPORTS : author_id
    USERS o|--o{ REPORTS : resolved_by
```

`AUTH_ACCOUNTS(provider, provider_user_id)` имеет составное ограничение UNIQUE.
В ERD оба столбца отмечены `UK`, но уникальность действует на их пару.

`REPORTS(target_type, target_id)` — полиморфная ссылка на пользователя, событие
или компанию. Физического внешнего ключа для `target_id` нет; существование цели
проверяет приложение.

Текущие `CHECK` ограничивают:

- `events.status`: pending, active, rejected, blocked, completed;
- `companies.join_type`: open, request;
- `companies.status`: active, closed, blocked;
- `applications.status`: pending, approved, rejected, cancelled;
- `event_participants.participation_type`: solo, company;
- `reports.target_type`: user, company, event;
- `reports.status`: pending, resolved, rejected.

Начальная схема также создаёт девять индексов: события по городу/дате, категории
и статусу; компании по событию и владельцу; заявки по компании/статусу и
пользователю; участие по пользователю; жалобы по статусу. Индексы первичных и
уникальных ключей PostgreSQL создаёт автоматически.

Миграция `000003` делает ключевые ссылки, статус и timestamps события обязательными,
задаёт значения по умолчанию для статуса и timestamps, запрещает `ends_at <= starts_at`
и добавляет индексы публичного каталога и списка событий создателя.

Миграция `000004` добавляет `companies.deleted_at`, ограничивает `max_members`
значениями 2–100, фиксирует роли `owner/member`, делает ключевые поля компаний и
участия обязательными и запрещает участие типа `solo` с компанией либо типа
`company` без компании. Составной внешний ключ `(company_id, event_id)` не даёт
привязать участие к компании другого события.

Mermaid не показывает все признаки `NOT NULL` и значения по умолчанию. Для них
источником истины остаётся SQL-миграция, ссылка на которую приведена в начале.

## Дополнения целевой схемы MVP

Последующие миграции должны добавить:

- ограничения для `users.role` и `users.status`;
- частичный UNIQUE для одной pending-заявки на `(company_id, user_id)`;
- обязательность и значения по умолчанию для полей, которые заполняет приложение;
- индексы под сессии, личные списки и фоновые задачи.

`event_participants` уже имеет составной первичный ключ `(event_id, user_id)`,
поэтому отдельный UNIQUE для этой пары не нужен.

После добавления каждой миграции текущая ERD обновляется по фактической схеме.
