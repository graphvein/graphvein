CREATE FUNCTION ddl_log() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN END $$;
CREATE EVENT TRIGGER ddl_log ON ddl_command_end EXECUTE FUNCTION ddl_log();
DROP FUNCTION ddl_log();
