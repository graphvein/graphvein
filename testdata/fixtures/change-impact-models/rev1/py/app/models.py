from pydantic import BaseModel


class Address(BaseModel):
    street: str
    city: str


class UserBase(BaseModel):
    name: str


class UserIn(UserBase):
    password: str
    address: Address


class UserOut(UserBase):
    id: int


class Unused(BaseModel):
    note: str
