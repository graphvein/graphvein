"""Post-definition mutation sites and DDL hooks: rows X1 and U5."""

from typing import Optional

from sqlalchemy import DDL, CheckConstraint, Column, ForeignKey, Index, Integer, event
from sqlmodel import Field, SQLModel


class Audited(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    amount: int = 0


class Ledger(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


def install() -> None:
    table = Audited.__table__
    table.append_constraint(CheckConstraint("amount >= 0", name="ck_audited_amount"))
    table.append_column(Column("note_id", Integer, nullable=True))
    table.c.note_id.append_foreign_key(ForeignKey("ledger.id"))
    table.indexes.add(Index("ix_audited_note", table.c.note_id))
    Audited.__table__.c.amount.nullable = True
    event.listen(table, "after_create", DDL("SELECT 1"))


Index("ix_audited_amount", Audited.amount)


@event.listens_for(Ledger.__table__, "before_drop")
def before_drop(target, connection, **kw) -> None:
    pass


@event.listens_for(Ledger, "before_insert")
def before_insert(mapper, connection, target) -> None:
    pass


install()
