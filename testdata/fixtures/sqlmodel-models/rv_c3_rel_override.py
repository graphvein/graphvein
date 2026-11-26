"""Revision 1 case: a Relationship removes the inherited field (M5, C2)."""

from sqlmodel import Relationship

from typing import Optional

from sqlmodel import Field, SQLModel


class Owner(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class PetBase(SQLModel):
    owner: Optional[int] = None


class Pet(PetBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    owner_id: Optional[int] = Field(default=None, foreign_key="owner.id")
    owner: Optional[Owner] = Relationship()
