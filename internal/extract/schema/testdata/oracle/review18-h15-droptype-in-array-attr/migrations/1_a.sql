create type st as enum ('a');
create type pair as (s st[], n int);
drop type st;
