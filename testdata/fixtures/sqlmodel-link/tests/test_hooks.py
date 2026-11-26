"""A test-scope site with an unresolved receiver: it never touches production tables (L8)."""

from sqlalchemy import Column, Integer


def extend(table) -> None:
    table.append_column(Column("extra", Integer))
