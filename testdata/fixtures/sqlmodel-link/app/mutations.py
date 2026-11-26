"""Post-definition mutation sites that change facts the class shows (Revision 3.1, rows X1, L8)."""

from sqlalchemy import (
    CheckConstraint,
    Column,
    ForeignKey,
    ForeignKeyConstraint,
    Integer,
    PrimaryKeyConstraint,
    String,
)
from sqlmodel import Field, SQLModel


class Keyed(SQLModel, table=True):
    code: str = Field(primary_key=True)
    part: int = Field(sa_column=Column(Integer))
    note: str


class Linked(SQLModel, table=True):
    id: int = Field(primary_key=True)
    keyed_code: str


class Opaque(SQLModel, table=True):
    id: int = Field(primary_key=True)
    extra: int = Field(sa_column=Column(Integer))


class Swapped(SQLModel, table=True):
    id: int = Field(primary_key=True)
    size: int


class Bound(SQLModel, table=True):
    id: int = Field(primary_key=True)
    ref: str


class Pointed(SQLModel, table=True):
    id: int = Field(primary_key=True)
    target: str


class Checked(SQLModel, table=True):
    id: int = Field(primary_key=True)
    amount: int = Field(sa_column=Column(Integer))


# A primary key added after the class: part becomes a primary-key column and
# not nullable (its Column sets no nullable).
Keyed.__table__.append_constraint(PrimaryKeyConstraint("code", "part"))
# A foreign key added after the class.
Linked.__table__.append_constraint(ForeignKeyConstraint(["keyed_code"], ["keyed.code"]))
# The constraint is held in a variable: its kind is not proven.
opaque_key = PrimaryKeyConstraint("id", "extra")
Opaque.__table__.append_constraint(opaque_key)
# replace_existing replaces a column the class defines.
Swapped.__table__.append_column(Column("size", String(10)), replace_existing=True)
# A constructor with a bound column attaches itself to that column's table.
ForeignKeyConstraint([Bound.__table__.c.ref], ["keyed.code"])
# A foreign key appended to one column.
Pointed.__table__.c.target.append_foreign_key(ForeignKey("keyed.code"))
# A check constraint is proven to change neither the primary key nor the
# foreign keys.
Checked.__table__.append_constraint(CheckConstraint("amount > 0", name="ck_checked_amount"))


class Commented(SQLModel, table=True):
    id: int = Field(primary_key=True)


# A table attribute assignment: every column, the schema and the listed
# constraint elements are unknown.
Commented.__table__.comment = "audit"


# Revision 3.2: attribute assignments that may rename a table or a column.
class Renamed(SQLModel, table=True):
    id: int = Field(primary_key=True)


class Moved(SQLModel, table=True):
    id: int = Field(primary_key=True)


class Relabeled(SQLModel, table=True):
    id: int = Field(primary_key=True)
    label: str
    note: str


class Rekeyed(SQLModel, table=True):
    id: int = Field(primary_key=True)
    code: str


# The table's name or schema changes: neither class claims a table.
Renamed.__table__.name = "renamed_v2"
Moved.__table__.schema = "archive"
# A column renamed through a local bound to the table: that column is not
# claimed, the others stay.
relabeled = Relabeled.__table__
relabeled.c.label.name = "title"
# A column's key changes: that column is not claimed.
Rekeyed.__table__.c.code.key = "ref"
