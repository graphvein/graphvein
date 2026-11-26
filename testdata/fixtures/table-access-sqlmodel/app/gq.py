"""A file that imports neither sqlmodel nor sqlalchemy (gate GQ)."""
from app.models import Team, User


def create_user(db_session, name):
    db_session.add(User(name=name))
    return db_session.get(Team, 1)
