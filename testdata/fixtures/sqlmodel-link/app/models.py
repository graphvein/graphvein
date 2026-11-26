"""Model tables linked across modules: chains, foreign keys, table args (rows L1-L5)."""

import uuid
from typing import Optional

from sqlalchemy import CheckConstraint, Column, ForeignKey, Index, String
from sqlmodel import Field, Relationship, SQLModel

from app.base import TimestampBase


class Team(TimestampBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    name: str = Field(index=True, max_length=50)
    heroes: list["Hero"] = Relationship(back_populates="team")


class Hero(SQLModel, table=True):
    __tablename__ = "heroes"
    id: uuid.UUID = Field(default_factory=uuid.uuid4, primary_key=True)
    team_id: Optional[int] = Field(default=None, foreign_key="team.id", ondelete="SET NULL")
    rival_id: Optional[int] = Field(default=None, foreign_key="nowhere.id")
    nickname: Optional[str] = Field(default=None, sa_column=Column(String(30), unique=True))
    mentor_id: Optional[uuid.UUID] = Field(default=None, sa_column=Column(ForeignKey("heroes.id", ondelete="CASCADE")))
    team: Optional[Team] = Relationship(back_populates="heroes")


class Tagged(SQLModel, table=True):
    __table_args__ = (
        Index("ix_tagged_code", "code", unique=True),
        CheckConstraint("length(code) > 1", name="ck_tagged_code"),
    )
    id: Optional[int] = Field(default=None, primary_key=True)
    code: str = Field(max_length=8)
