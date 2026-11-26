"""Every CT-P1..CT-P5 form and refusal the fixture covers."""
from typing import Annotated, Optional

import sqlalchemy as sa
from fastapi import Depends
from sqlalchemy import func, text
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.orm import aliased
from sqlmodel import Session, col, delete, insert, select, update
from sqlmodel import select as sm_select

from app.models import Item, ItemCreate, Team, User

SessionDep = Annotated[Session, Depends(lambda: None)]

ALL_TEAMS = select(Team)


def p1_entities(session: Session):
    session.exec(select(User)).all()
    session.exec(select(User.name, col(Team.id)))
    boss = aliased(User)
    session.exec(select(aliased(Team)))
    session.exec(sm_select(Item))
    session.exec(sa.select(Team))
    return boss


def p1_chains(session: Session):
    session.exec(select(User).join(Team, User.team_id == Team.id).outerjoin(Item))
    session.exec(select(func.count()).select_from(Team))
    session.exec(select(User.id).join_from(User, Team))


def p1_refusals(session: Session, alias):
    session.exec(select(func.count()))
    session.exec(select(User, func.max(Item.id)))
    session.exec(select(alias))


def p1_builder():
    return select(Item).where(Item.id > 1)


def p2_writes(session: Session):
    session.exec(insert(User).values(name="a"))
    session.exec(update(Team).where(Team.id == 1).values(name="b"))
    session.exec(delete(Item).where(Item.id == 2))
    session.exec(pg_insert(User).values(id=1, name="c").on_conflict_do_update(index_elements=["id"], set_={"name": "c"}))
    session.exec(sa.insert(Item).from_select(["title"], select(Team.name)))
    session.exec(insert(sa.table("audit")).values(x=1))


def p3_gets(session: Session, db: SessionDep, other, key):
    session.get(User, 1)
    session.get_one(Team, 2)
    db.get(Item, 3)
    other.get(User, 4)
    session.get(key, 5)
    d = {}
    d.get("x")


class Repository:
    def __init__(self, session):
        self.session = session

    def find(self, item_id: int):
        return self.session.get(Item, item_id)


def p4_objects(session: Session, team: Team | None, item: Optional[Item], payload: ItemCreate, rows, loose):
    session.add(User(name="a"))
    session.add(Item.model_validate(payload))
    session.add(Item.from_orm(payload))
    user = User(name="b")
    session.add(user)
    session.merge(team)
    session.delete(item)
    session.add_all([User(name="c"), Team(name="d")])
    for row in rows:
        session.refresh(row)
    session.add_all(rows)
    session.add(loose)
    session.add(payload)


def p4_unannotated(session, team):
    session.refresh(team)
    seen = set()
    seen.add(team)


def p5_text(session: Session, name: str):
    session.exec(text("SELECT id, name FROM teams WHERE id = :id"))
    session.execute(sa.text('UPDATE "user" SET name = :name WHERE id IN (SELECT owner_id FROM item)'))
    session.exec(text("select * from missing_table"))
    session.exec(text(f"select * from teams where name = '{name}'"))
    session.exec(text("DELETE FROM item; SELECT 1"))
    session.exec(text("select now()"))


def executions(session: Session, stmt):
    built = select(Team)
    session.exec(built)
    session.exec(stmt)
    session.exec(p1_builder())


def executions_local(session: Session, flag: bool):
    # R5-A1: one stable assignment in this scope, executed through the
    # local: the site executes and no statement_not_traced record.
    statement = select(User).where(User.id == 1)
    session.exec(statement).all()
    # Assigned twice, or only conditionally: not traced (builds, and the
    # execution is statement_not_traced).
    again = select(Team)
    again = select(Item)
    session.exec(again)
    if flag:
        maybe = delete(Item)
    session.exec(maybe)


def shadowed(session: Session):
    # R5-B2: except ... as and with ... as bind the name in this scope, so
    # User and Team here are not the model classes: unknowns, no facts.
    try:
        pass
    except Exception as User:
        session.exec(delete(User))
    with open("x") as Team:
        return select(Team)


def keyword_tables(session: Session):
    # R5-B1: unquoted user in FROM is Postgres's current_user function, no
    # table; quoted "user" is the table.
    session.exec(text("select * from user"))
    session.exec(text('select id from "user"'))


def write_predicates(session: Session):
    # R5-C3: a write whose predicate names another model in a form the
    # payload keeps is site_partial with that model as candidate; inside a
    # comparison the payload does not keep it (documented gap).
    session.exec(update(User).where(Item.title.is_not(None)).values(name="x"))
    session.exec(update(User).where(User.id == Item.owner_id).values(name="y"))
