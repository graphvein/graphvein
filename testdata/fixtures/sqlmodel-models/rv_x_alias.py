"""Revision 1 derived case: Optional alias, plain-validator aliases, TypeAlias with Field metadata."""

from typing import Annotated, Optional, TypeAlias

from pydantic import AfterValidator, BeforeValidator
from sqlmodel import Field, SQLModel


def same(value):
    return value


MaybeText = Optional[str]
Checked = Annotated[str, AfterValidator(same)]
Before = BeforeValidator(same)
Prepared = Annotated[str, Before]
Indexed: TypeAlias = Annotated[str, Field(index=True)]


class Validated(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    maybe: MaybeText = None
    checked: Checked
    prepared: Prepared
    indexed: Indexed
