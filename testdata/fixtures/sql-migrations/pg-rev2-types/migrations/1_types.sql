-- G1: DROP TYPE ... CASCADE drops the columns of that type.
CREATE TYPE mood AS ENUM ('ok', 'sad');
CREATE TABLE feelings (id integer, current mood, history mood[]);
CREATE TABLE unrelated (id integer, label text);
DROP TYPE mood CASCADE;
