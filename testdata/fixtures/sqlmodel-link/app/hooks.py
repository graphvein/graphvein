"""Mutation sites resolved through a local binding and a DDL hook (rows X1, U5, L8)."""

from sqlalchemy import DDL, CheckConstraint, event

from app.models import Hero, Tagged


def install() -> None:
    table = Tagged.__table__
    table.append_constraint(CheckConstraint("id > 0", name="ck_tagged_id"))


event.listen(Hero.__table__, "after_create", DDL("SELECT 1"))
install()
