"""SQLModel session queries on the model tables (T019 Revision 4, I8)."""
from sqlalchemy import func
from sqlmodel import Session, select

from .models import Hero, Team


def heroes(session: Session):
    return session.exec(select(Hero)).all()


def hero_by_id(session, hero_id: int):
    return session.get(Hero, hero_id)


def largest_team(session: Session):
    return session.exec(select(Team, func.max(Hero.id))).one()
