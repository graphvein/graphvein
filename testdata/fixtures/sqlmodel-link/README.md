# sqlmodel-link fixture

Own code for T018c2 (model linking, `docs/extract/schema-models.md`, ADR
0054): model tables whose chain, foreign keys and mutation sites cross
modules, and a sqlx migration set that defines a table of the same name.

| file | rows |
|---|---|
| `app/base.py`, `app/models.py` | C2 and L2 across modules, N1, N2, F1-F5, F7, K1-K3, TA2, TA3, M5, L4 (resolved, self-reference, unresolved) |
| `app/external.py` | R-B2/L2: a base outside the repository, no table |
| `app/broken.py` | P1/L7: `pattern=` is outside `sqlmodel.Field`'s signature (expected import failure) |
| `app/core/mixins.py`, `app/squads.py` | G3 (Revision 3): a table whose base is a same-file subclass of a mixin in another module |
| `app/pairs.py` | L7 (Revision 3): a `PrimaryKeyConstraint` in `__table_args__`; the tables import, the primary key is unknown |
| `app/fail_no_pk.py` | P1/L7: no primary key, and `__table_args__` holds only a `UniqueConstraint` (expected import failure) |
| `app/fail_m12_named.py`, `app/fail_m12_mixin.py` | P1/L7: a non-annotated literal after `__tablename__`, and in a non-table mixin (expected import failures) |
| `app/m12_classvar.py` | M12 without P1: the literal rebinds a base's `ClassVar` (imports) |
| `app/declarative.py` | U1: a declarative base and its subclass, both counted |
| `app/hooks.py` | X1 through a local `table = Tagged.__table__`, U5 on `Hero.__table__` |
| `app/mutations.py` | X1 (Revision 3.1): post-definition sites that change facts the class shows (an added `PrimaryKeyConstraint`, `ForeignKeyConstraint`, a constraint in a variable, `replace_existing=True`, a bound-column constructor, `append_foreign_key`, a table attribute assignment) and a `CheckConstraint` that changes neither key; Revision 3.2: attribute assignments that may rename (`Renamed.__table__.name`, `Moved.__table__.schema`: no table claimed; `relabeled.c.label.name` through a local, `Rekeyed.__table__.c.code.key`: that column not claimed) |
| `app/decorators.py`, `app/decorators_shadowed.py` | M1 and L1 (Revision 3.3, G-1): `@classmethod` alone, `@field_validator` over `@classmethod`, `@staticmethod` and `@property` resolve to the builtins (tables read); a module-level `classmethod = builtins.classmethod` shadows the builtin, so that method stays an M1 refusal |
| `tests/test_hooks.py` | L8: a test-scope site with an unresolved receiver (no effect on production tables) |
| `Cargo.toml`, `migrations/0001_init.sql` | L5: the migration set's `team` and the model `team` are compared, not merged |

Oracle (T018c2, Oracle gate), from a scratch venv with the V1 pins, outside
the repository; `app.external` needs a stub `vendor_models` and is left out:

```sh
python -I -B scripts/oracle/sqlmodel_metadata.py --root testdata/fixtures/sqlmodel-link \
  --path . --isolate app.models app.hooks app.broken app.squads app.pairs \
  app.fail_no_pk app.fail_m12_named app.fail_m12_mixin app.m12_classvar \
  app.declarative app.mutations app.decorators app.decorators_shadowed \
  > "$SCRATCH/link-oracle.json"
```

Expected: 21 tables and 47 columns (`team`, `heroes`, `tagged`, `squad`,
`pair`, `slot`, `patch`, `unkeyed`, which SQLAlchemy builds before the
mapper fails, `schedule`, `checked` and `shadowed`, and ten tables of
`app.mutations`: the harness skips
`Renamed` and `Moved`, whose key no longer matches `SQLModel.metadata`
after the rename);
`app.broken` fails with `TypeError`, `app.fail_no_pk` with
`ArgumentError`, `app.fail_m12_named` and `app.fail_m12_mixin` with
`PydanticUserError`. `app.declarative` uses its own metadata, so it adds
no table to `SQLModel.metadata`.
