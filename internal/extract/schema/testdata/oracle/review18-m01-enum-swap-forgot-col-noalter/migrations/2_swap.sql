ALTER TYPE status RENAME TO status_old;
CREATE TYPE status AS ENUM ('a', 'b', 'c');
DROP TYPE status_old;
