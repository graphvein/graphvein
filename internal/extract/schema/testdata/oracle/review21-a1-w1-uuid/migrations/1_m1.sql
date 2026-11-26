CREATE EXTENSION "uuid-ossp"; CREATE TABLE t (id uuid DEFAULT uuid_generate_v4(), b int);
