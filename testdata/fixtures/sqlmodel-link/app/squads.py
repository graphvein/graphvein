"""A table whose base is a same-file subclass of a mixin in another module (Revision 3, G3)."""

from sqlmodel import Field

from app.core.mixins import IDMixin


class Squad(IDMixin, table=True):
    callsign: str = Field(max_length=12)
