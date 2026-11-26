"""Expected import failure. Row M12: an unannotated assignment pydantic rejects."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Loose(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    loose = 5
