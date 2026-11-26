from sqlmodel import Session, select

from app.models import User


def test_list_users(session: Session):
    assert session.exec(select(User)).all() == []
