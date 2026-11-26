create table post_aggregates (id serial primary key, post_id int, comments bigint default 0);
create table comment (id serial primary key, post_id int);
create function post_aggregates_comment_count() returns trigger language plpgsql as $$
begin
  IF (TG_OP = 'INSERT') THEN
    update post_aggregates pa set comments = comments + 1 where pa.post_id = NEW.post_id;
  END IF;
  return null;
end $$;
create trigger post_aggregates_comment_count after insert or delete on comment for each row execute procedure post_aggregates_comment_count();
