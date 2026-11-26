-- R1: every serial alias, any case, against plain integer columns.
CREATE TABLE counters (
    a smallserial,
    b SERIAL2,
    c serial PRIMARY KEY,
    d Serial4,
    e BIGSERIAL,
    f serial8,
    g integer,
    h integer DEFAULT 0,
    i "serial"
);

-- DML in a set that defines no function or trigger has no effect.
INSERT INTO counters (g) VALUES (1);
