CREATE TABLE "User" (id serial PRIMARY KEY, "updatedAt" timestamp);
CREATE FUNCTION "touchUser"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW."updatedAt" = now(); RETURN NEW; END $$;
CREATE TRIGGER "touchUser" BEFORE UPDATE ON "User" FOR EACH ROW EXECUTE FUNCTION "touchUser"();
DROP FUNCTION "touchUser"();
