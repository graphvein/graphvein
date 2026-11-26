# sqlmodel-models fixture

Own code (no third-party text) for T018c1: the SQLModel/SQLAlchemy model
syntax capture (`docs/lang/python-models.md`, ADR 0053). One module per row
group of the claim contract, with a passing and a refused variant of each
row, plus the Revision 1 synthetic cases (`rv_*`, reconstructed from the
task's descriptions in Revision 1, R8) and the investigation cases (`cases.py`).

| module | rows |
|---|---|
| `c_headers.py` | C1, C1b, C1c, C2, C5 (nested `Config`, dict `model_config`), C6, conditional and decorated classes (D) |
| `c3_table_base.py`, `c4_registry.py` | C3 (does not raise: the subclass is not a table), C4 |
| `n_names.py` | N1, N2, N3 (non-literal name, non-ASCII class name), inherited `__tablename__` |
| `m_members.py` | M0-M10, M12 (an `if` block in a class body) |
| `a_annotations.py` | A1, A2, A5 aliases, A1m (plain validator and `pydantic.Field` metadata) |
| `f_field.py` | F1-F9 including non-literal values, `sa_column=<helper>()`, an unknown signature keyword (`regex`) |
| `k_column.py`, `t_types.py` | K1-K6 (PF1's `Column(ForeignKey(...))`, opaque K1b types), T1; `t_types.py` also holds one oracle case for every claimed type form (T018g0 Revision 1: bare `Numeric`, `Numeric(10, 2)`, `Decimal`, every other T1 class and every F5 source type, Revision 2: `EmailStr`, `Path`, the four `ipaddress` types and enums on `StrEnum`, `IntEnum`, `Flag`, `IntFlag`) |
| `ta_table_args.py` | M11, TA1-TA3, TA-D, options dicts |
| `u_unsupported.py`, `u2_mapped.py` | U1, U2, U3, U4 |
| `x_sites.py` | X1 sites and U5 DDL hooks (and an ORM event that is not one) |
| `fail_*.py` | the import-failing variants of A5, C5, F4 (`SET NULL`), F5, F7r, F9, M12 |

Expected import failures (isolated, one module each, so they cannot stop
the others): `fail_a5_later.py`, `fail_c5_config.py`, `fail_f4_set_null.py`, `fail_f5_list.py`, `fail_f7r.py`, `fail_f9_signature.py`, `fail_m12_plain.py`, `rv_c2_dup_name.py`, `rv_c2_no_pk.py`, `rv_c2_shared_sa_column.py`, `rv_c2_shared_table_args.py`, `rv_c3_list.py`, `rv_c3_literal.py`, `rv_c3_union.py`.

## Oracle harness (for T018c2)

Each module is imported alone in a fresh interpreter (they share the
default `SQLModel.metadata`, so importing them together would collide on
table names), with the V1 pins in a scratch venv outside the repository:

```sh
python3 -m venv "$SCRATCH/venv"
"$SCRATCH/venv/bin/pip" install sqlmodel==0.0.39 sqlalchemy==2.0.51 pydantic==2.13.4 email-validator==2.3.0
cd "$SCRATCH"   # not inside the fixture: -B and a foreign cwd write nothing here
for f in /path/to/testdata/fixtures/sqlmodel-models/*.py; do
  m=$(basename "$f" .py)
  PYTHONPATH=/path/to/testdata/fixtures/sqlmodel-models "$SCRATCH/venv/bin/python" -B -c "
import importlib, sys
try:
    importlib.import_module('$m'); err = None
except Exception as e:
    err = type(e).__name__
from sqlmodel import SQLModel
t = SQLModel.metadata.tables
print('$m', len(t), sum(len(x.columns) for x in t.values()), err)"
done
```

Expected (2026-11-09, Python 3.13.18 with the V1 pins): 45 modules, 31
import, 51 tables and 177 columns in the importing modules' metadata. Per
module (tables and columns present in `SQLModel.metadata` when the import
stops; a failing module can leave earlier tables behind):

| module | tables | columns | import |
|---|---|---|---|
| `a_annotations.py` | 1 | 11 | imports |
| `c3_table_base.py` | 1 | 1 | imports |
| `c4_registry.py` | 0 | 0 | imports |
| `c_headers.py` | 6 | 7 | imports |
| `cases.py` | 1 | 4 | imports |
| `f_field.py` | 2 | 21 | imports |
| `fail_a5_later.py` | 0 | 0 | expected import failure: `TypeError` |
| `fail_c5_config.py` | 0 | 0 | expected import failure: `AttributeError` |
| `fail_f4_set_null.py` | 1 | 1 | expected import failure: `RuntimeError` |
| `fail_f5_list.py` | 0 | 0 | expected import failure: `ValueError` |
| `fail_f7r.py` | 0 | 0 | expected import failure: `RuntimeError` |
| `fail_f9_signature.py` | 0 | 0 | expected import failure: `TypeError` |
| `fail_m12_plain.py` | 0 | 0 | expected import failure: `PydanticUserError` |
| `k_column.py` | 2 | 14 | imports |
| `m_members.py` | 2 | 11 | imports |
| `n_names.py` | 5 | 6 | imports |
| `rv_c1.py` | 1 | 2 | imports |
| `rv_c2_decimal_ann.py` | 1 | 2 | imports |
| `rv_c2_dup_name.py` | 1 | 1 | expected import failure: `InvalidRequestError` |
| `rv_c2_fk_schema.py` | 2 | 3 | imports |
| `rv_c2_multi_base_table.py` | 1 | 1 | imports |
| `rv_c2_no_pk.py` | 1 | 1 | expected import failure: `ArgumentError` |
| `rv_c2_override.py` | 1 | 2 | imports |
| `rv_c2_pk_nullable.py` | 1 | 2 | imports |
| `rv_c2_satype_union.py` | 1 | 2 | imports |
| `rv_c2_shared_sa_column.py` | 1 | 2 | expected import failure: `ArgumentError` |
| `rv_c2_shared_table_args.py` | 1 | 2 | expected import failure: `ArgumentError` |
| `rv_c3_colname.py` | 1 | 2 | imports |
| `rv_c3_int_none.py` | 1 | 2 | imports |
| `rv_c3_list.py` | 0 | 0 | expected import failure: `ValueError` |
| `rv_c3_literal.py` | 0 | 0 | expected import failure: `TypeError` |
| `rv_c3_maxlen0.py` | 1 | 2 | imports |
| `rv_c3_rel_override.py` | 2 | 3 | imports |
| `rv_c3_tablename_chain_annotated.py` | 1 | 1 | imports |
| `rv_c3_union.py` | 0 | 0 | expected import failure: `ValueError` |
| `rv_c4.py` | 1 | 2 | imports |
| `rv_x_alias.py` | 1 | 5 | imports |
| `rv_x_int_none.py` | 1 | 3 | imports |
| `rv_x_shared_uq.py` | 2 | 4 | imports |
| `rv_x_single_inherit.py` | 1 | 3 | imports |
| `t_types.py` | 4 | 49 | imports |
| `ta_table_args.py` | 3 | 6 | imports |
| `u2_mapped.py` | 2 | 2 | imports |
| `u_unsupported.py` | 0 | 0 | imports |
| `x_sites.py` | 2 | 4 | imports |

## Goldens

- `../sqlmodel-models.golden.json`: the graph records (existing Python
  symbol records only; T018c1 adds no record kind).
- `payload.golden.json`: the cached `Models` payload of every file, by
  path. It is removed from the indexed copy, so it is not a fixture input.

Both are checked by `TestSQLModelFixture` (`internal/lang/python`);
`TestModelPayloadContract` checks that capturing these modules emits
exactly the contract rows with a capture side. No ignore files or ignored
paths are used, so no inert renames are needed.
