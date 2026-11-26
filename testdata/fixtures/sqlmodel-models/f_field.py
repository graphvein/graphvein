"""Field(...) arguments: rows F1-F9 (passing and refused variants)."""

import datetime
import decimal
import enum
import uuid
from typing import Optional

from sqlalchemy import JSON, Column, String
from sqlmodel import Field, SQLModel

FLAG = False
DEFAULT_NAME = "anon"


def now() -> datetime.datetime:
    return datetime.datetime.now()


def text_column():
    return Column(String(10))


class Color(str, enum.Enum):
    red = "red"
    blue = "blue"


class Owner(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Fields(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    code: str = Field(index=True, unique=True, nullable=False, max_length=12)
    flagged: Optional[str] = Field(default=None, index=FLAG)
    name: str = Field(default=DEFAULT_NAME)
    count: int = Field(0)
    required: str = Field(default=...)
    created: datetime.datetime = Field(default_factory=now)
    stamped: datetime.datetime = Field(default_factory=lambda: datetime.datetime.now())
    token: uuid.UUID = Field(default_factory=uuid.uuid4)
    owner_id: Optional[int] = Field(default=None, foreign_key="owner.id", ondelete="CASCADE")
    other_owner: Optional[int] = Field(default=None, foreign_key=DEFAULT_NAME)
    color: Color = Field(default=Color.red)
    price: decimal.Decimal = Field(default=0, max_digits=10, decimal_places=2)
    zero: str = Field(default="", max_length=0)
    payload: Optional[dict] = Field(default=None, sa_type=JSON)
    column: Optional[str] = Field(default=None, sa_column=Column(String(20), nullable=True))
    helper: Optional[str] = Field(default=None, sa_column=text_column())
    extras: Optional[str] = Field(default=None, sa_column_kwargs={"comment": "c"})
    described: str = Field(default="", description="shown in the API", title="T", min_length=0)
    discriminated: Optional[str] = Field(default=None, regex="^a")
