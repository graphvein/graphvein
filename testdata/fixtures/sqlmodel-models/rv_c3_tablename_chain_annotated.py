"""Revision 1 case: an annotated __tablename__ in the chain (N1 refusal, N3)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class NamedBase(SQLModel):
    __tablename__: str = "annotated_name"


class NamedChild(NamedBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
