"""K-dec builtins (Revision 3.3, G-1): classmethod, staticmethod and property
resolve to the builtins when no binding shadows them, so the tables are read."""

from typing import Optional

from pydantic import field_validator
from sqlmodel import Field, SQLModel


class Schedule(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    label: str

    @classmethod
    def make(cls, label: str) -> "Schedule":
        return cls(label=label)


class Checked(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    code: str

    @field_validator("code")
    @classmethod
    def upper(cls, value: str) -> str:
        return value.upper()

    @staticmethod
    def width() -> int:
        return 8

    @property
    def short(self) -> str:
        return self.code[:2]
