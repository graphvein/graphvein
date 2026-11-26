"""Expected import failure. Row F7r: sa_column together with a flag keyword."""

from typing import Optional

from sqlalchemy import Column, Integer
from sqlmodel import Field, SQLModel


class Clash(SQLModel, table=True):
    id: Optional[int] = Field(default=None, sa_column=Column(Integer, primary_key=True), primary_key=True)
