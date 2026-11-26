"""Expected import failure. Row C5: a model_config key outside K-cfg (registry)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Strict(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    model_config = {"table": True, "registry": None}
