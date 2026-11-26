from datetime import datetime
from typing import Optional

from sqlalchemy import Column, DateTime, Index, text
from sqlmodel import Field, SQLModel


class Team(SQLModel, table=True):
    __tablename__ = "teams"
    id: Optional[int] = Field(default=None, primary_key=True)
    name: str


class User(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    name: str
    team_id: Optional[int] = Field(default=None, foreign_key="teams.id")
    # A DDL fragment: text() here is no query site.
    created_at: Optional[datetime] = Field(default=None, sa_column=Column(DateTime, server_default=text("now()")))


class Item(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    title: str
    owner_id: Optional[int] = Field(default=None, foreign_key="user.id")


class ItemCreate(SQLModel):
    title: str


Index("ix_item_title", Item.title, postgresql_where=text("title <> ''"))
