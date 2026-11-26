from sqlmodel import Field, SQLModel


class HeroBase(SQLModel):
    name: str
    secret_name: str


class Hero(HeroBase, table=True):
    id: int | None = Field(default=None, primary_key=True)


class HeroPublic(HeroBase):
    id: int


# The same fields as the Hero table, declared again: a copy, not the
# table's model.
class HeroCopy(SQLModel):
    id: int | None = None
    name: str
    secret_name: str


class Team(SQLModel, table=True):
    id: int | None = Field(default=None, primary_key=True)
    name: str


class TeamList(SQLModel):
    data: list[Team]
    count: int


class Audit(SQLModel, table=True):
    id: int | None = Field(default=None, primary_key=True)
    note: str
