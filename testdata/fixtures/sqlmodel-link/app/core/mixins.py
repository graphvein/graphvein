"""Same-file mixin chain (Revision 3, gate G3): IDMixin subclasses a SQLModel mixin."""

from datetime import datetime
from typing import Optional

from sqlmodel import Field, SQLModel


class TimestampMixin(SQLModel):
    updated: Optional[datetime] = None


class IDMixin(TimestampMixin):
    id: Optional[int] = Field(default=None, primary_key=True)
