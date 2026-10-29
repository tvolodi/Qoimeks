insert into skus (code, name, unit) values
    ('BLT-M8-40',  'Болт М8х40 оцинкованный',        'шт'),
    ('BLT-M10-60', 'Болт М10х60 оцинкованный',       'шт'),
    ('NUT-M8',     'Гайка М8',                       'шт'),
    ('NUT-M10',    'Гайка М10',                      'шт'),
    ('WSH-8',      'Шайба плоская 8',                'шт'),
    ('SCR-4-40',   'Саморез 4х40 по дереву',         'кг'),
    ('PNT-WH-10',  'Краска белая водоэмульсионная 10 л', 'шт'),
    ('PNT-GR-3',   'Краска серая эмаль 3 л',         'шт'),
    ('CBL-3x2.5',  'Кабель ВВГ 3х2,5',               'м'),
    ('CBL-3x1.5',  'Кабель ВВГ 3х1,5',               'м'),
    ('GLV-L',      'Перчатки рабочие L',             'пар'),
    ('TAPE-48',    'Скотч упаковочный 48 мм',        'шт');

insert into locations (code, name) values
    ('A-01', 'Ряд A, ячейка 1'),
    ('A-02', 'Ряд A, ячейка 2'),
    ('A-03', 'Ряд A, ячейка 3'),
    ('B-01', 'Ряд B, ячейка 1'),
    ('B-02', 'Ряд B, ячейка 2'),
    ('C-01', 'Ряд C, ячейка 1'),
    ('RAMP', 'Рампа приёмки');

-- Every demo SKU has a home location, so the operation screen offers one straight away.
update skus s set location_id = l.id
from (values
    ('BLT-M8-40', 'A-01'), ('BLT-M10-60', 'A-02'), ('NUT-M8', 'A-03'), ('NUT-M10', 'B-01'),
    ('WSH-8', 'B-02'), ('SCR-4-40', 'C-01'), ('PNT-WH-10', 'C-01'), ('PNT-GR-3', 'C-01'),
    ('CBL-3x2.5', 'B-01'), ('CBL-3x1.5', 'B-01'), ('GLV-L', 'A-01'), ('TAPE-48', 'RAMP')
) as m(sku, loc)
join locations l on l.code = m.loc
where s.code = m.sku;
