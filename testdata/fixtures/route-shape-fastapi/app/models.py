from enum import Enum
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, computed_field, field_serializer, model_validator


class Color(str, Enum):
    red = "red"
    blue = "blue"


class Message(BaseModel):
    message: str


class ItemBase(BaseModel):
    name: str
    price: float = 1.0
    tags: list[str] = []
    note: str | None


class ItemCreate(ItemBase):
    color: Color = Color.red
    size: Literal["s", "m"] = Field(default="s")


class Item(ItemBase):
    id: int
    owner: "Owner | None" = None


class Owner(BaseModel):
    model_config = ConfigDict(extra="forbid")

    login: str = Field(alias="userName")
    email: str = Field(..., max_length=255)
    items: list[Item] = Field(default_factory=list)


class Computed(BaseModel):
    a: int

    @computed_field
    @property
    def double(self) -> int:
        return self.a * 2


def to_upper(name: str) -> str:
    return name.upper()


class Generated(BaseModel):
    model_config = ConfigDict(alias_generator=to_upper)

    value: int


class Serialized(BaseModel):
    when: int

    @field_serializer("when")
    def show(self, value: int) -> str:
        return str(value)


class Reshaped(BaseModel):
    name: str

    @model_validator(mode="before")
    @classmethod
    def accept_bare(cls, data):
        return {"name": data} if isinstance(data, str) else data
