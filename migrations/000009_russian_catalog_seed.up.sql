BEGIN;

INSERT INTO cities (name, slug) VALUES
    ('Москва', 'moskva'),
    ('Санкт-Петербург', 'sankt-peterburg'),
    ('Новосибирск', 'novosibirsk'),
    ('Екатеринбург', 'ekaterinburg'),
    ('Казань', 'kazan'),
    ('Нижний Новгород', 'nizhniy-novgorod'),
    ('Красноярск', 'krasnoyarsk'),
    ('Челябинск', 'chelyabinsk'),
    ('Самара', 'samara'),
    ('Уфа', 'ufa'),
    ('Ростов-на-Дону', 'rostov-na-donu'),
    ('Краснодар', 'krasnodar'),
    ('Омск', 'omsk'),
    ('Воронеж', 'voronezh'),
    ('Пермь', 'perm'),
    ('Волгоград', 'volgograd'),
    ('Саратов', 'saratov'),
    ('Тюмень', 'tyumen'),
    ('Тольятти', 'tolyatti'),
    ('Ижевск', 'izhevsk')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO event_categories (name, slug) VALUES
    ('Концерты и музыка', 'music'),
    ('Театр и спектакли', 'theatre'),
    ('Кино', 'cinema'),
    ('Выставки и музеи', 'exhibitions'),
    ('Фестивали', 'festivals'),
    ('Спорт', 'sport'),
    ('Прогулки и экскурсии', 'walks-and-tours'),
    ('Образование', 'education'),
    ('Игры и квизы', 'games-and-quizzes'),
    ('Вечеринки', 'parties'),
    ('Нетворкинг', 'networking'),
    ('Волонтёрство', 'volunteering')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO interests (name, slug) VALUES
    ('Живая музыка', 'live-music'),
    ('Кино', 'cinema'),
    ('Театр', 'theatre'),
    ('Современное искусство', 'modern-art'),
    ('История', 'history'),
    ('Путешествия', 'travel'),
    ('Бег', 'running'),
    ('Велоспорт', 'cycling'),
    ('Фитнес', 'fitness'),
    ('Футбол', 'football'),
    ('Настольные игры', 'board-games'),
    ('Квизы', 'quizzes'),
    ('Книги', 'books'),
    ('Фотография', 'photography'),
    ('Технологии', 'technology'),
    ('Предпринимательство', 'business'),
    ('Иностранные языки', 'languages'),
    ('Танцы', 'dance'),
    ('Гастрономия', 'food'),
    ('Волонтёрство', 'volunteering')
ON CONFLICT (slug) DO NOTHING;

COMMIT;
