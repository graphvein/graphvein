from fastapi import FastAPI

from app import other
from app.models import Hero, HeroCopy, HeroPublic, Team, TeamList

app = FastAPI()


@app.post("/heroes", response_model=HeroPublic)
def create_hero(hero: Hero):
    return hero


@app.get("/heroes/{hero_id}")
def read_hero(hero_id: int) -> Hero:
    return Hero(id=hero_id, name="a", secret_name="b")


@app.get("/heroes")
def list_heroes() -> list[Hero]:
    return []


@app.get("/copies/{copy_id}")
def read_copy(copy_id: int) -> HeroCopy:
    return HeroCopy(name="a", secret_name="b")


@app.get("/others/{other_id}")
def read_other(other_id: int) -> other.Hero:
    return other.Hero(id=other_id, name="a")


@app.get("/teams")
def list_teams() -> TeamList:
    return TeamList(data=[], count=0)


@app.put("/teams/{team_id}")
def replace_team(team_id: int, team: Team) -> Team | None:
    return team
