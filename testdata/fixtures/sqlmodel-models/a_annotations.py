"""Annotations: rows A1, A2, A3, A5 and A1m."""

import datetime
import uuid
from typing import Annotated, Optional, Union

from pydantic import BeforeValidator
from pydantic import Field as PydanticField
from sqlmodel import Field, SQLModel


def strip(value):
    return value


Stripped = Annotated[str, BeforeValidator(strip)]
OptStr = Optional[str]
Limited = Annotated[str, PydanticField(max_length=5)]


class Annotations(SQLModel, table=True):
    id: uuid.UUID = Field(default_factory=uuid.uuid4, primary_key=True)
    pipe: int | None = None
    optional: Optional[str] = None
    union: Union[int, None] = None
    plain: str
    when: datetime.datetime
    stripped: Stripped
    alias: OptStr = None
    validated: Annotated[str, BeforeValidator(strip)]
    limited: Limited
    meta: Annotated[int, Field(index=True)] = 0
