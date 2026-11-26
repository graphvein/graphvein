# table-access-sqlmodel

Fixture for T018h rows CT-P1..CT-P5 (SQLModel / SQLAlchemy query sites)
and the Python execution records of row D. `app/models.py` holds the
SQLModel group (`teams`, `user`, `item`); `app/crud.py` holds every form
and refusal per row; `app/gq.py` imports neither library (gate GQ);
`alembic/versions/0001_backfill.py` is an Alembic-like data migration read
as ordinary Python; `tests/` holds a test-scope site.

Revision 5 cases at the end of `app/crud.py`: `executions_local` (R5-A1:
a statement executed through a local with one stable assignment executes;
a twice-assigned or conditional local does not), `shadowed` (R5-B2:
`except ... as User` and `with ... as Team` rebind the model names:
unknowns, no facts), `keyword_tables` (R5-B1: `select * from user` claims
nothing, `"user"` claims the table) and `write_predicates` (R5-C3: a model
in a write's predicate the payload keeps is `site_partial`; one inside a
comparison is the documented gap).

No ignore files and no ignored paths: nothing needs an inert name.
