"""Revision 1 case c1 (A1m): SQLModel reads Field values from Annotated metadata."""

from typing import Annotated, Optional

from sqlmodel import Field, SQLModel


class AnnotatedPk(SQLModel, table=True):
    id: Annotated[Optional[int], Field(default=None, primary_key=True)]
    code: Annotated[str, Field(max_length=5, index=True)]
