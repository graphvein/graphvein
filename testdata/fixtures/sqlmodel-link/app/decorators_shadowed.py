"""A module-level binding shadows classmethod (Revision 3.3, G-1): the
decorator does not resolve to the builtin, so the method stays an M1 refusal
(column set partial)."""

import builtins
from typing import Optional

from sqlmodel import Field, SQLModel

classmethod = builtins.classmethod


class Shadowed(SQLModel, table=True):
    __tablename__ = "shadowed"
    id: Optional[int] = Field(default=None, primary_key=True)

    @classmethod
    def make(cls) -> "Shadowed":
        return cls()
