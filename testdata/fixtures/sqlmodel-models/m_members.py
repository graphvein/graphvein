"""Class-body members: rows M0-M10 and M12."""

import functools
from typing import ClassVar, Optional

from pydantic import ConfigDict, field_validator
from sqlmodel import Field, Relationship, SQLModel

LIMIT = 10


class Team(SQLModel, table=True):
    """Docstrings, pass and ellipsis are M0."""

    id: Optional[int] = Field(default=None, primary_key=True)
    name: str
    rank: int = 3
    label: str = "x"
    score: float = 1.5
    active: bool = True
    nothing: Optional[str] = None
    limit: int = LIMIT
    members: list["Member"] = Relationship(back_populates="team")
    registry_name: ClassVar[str] = "teams"
    _cache: Optional[dict] = None
    model_config = ConfigDict(validate_assignment=True, from_attributes=True)
    __mapper_args__ = {"confirm_deleted_rows": False}
    ...

    @field_validator("name")
    @classmethod
    def check_name(cls, value: str) -> str:
        return value

    @functools.cached_property
    def slow(self) -> int:
        return 1

    def plain(self) -> None:
        pass


class Member(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    team_id: Optional[int] = Field(default=None, foreign_key="team.id")
    team: Optional[Team] = Relationship(back_populates="members")

    if LIMIT > 5:
        level: int = 1
    else:
        level: int = 2
