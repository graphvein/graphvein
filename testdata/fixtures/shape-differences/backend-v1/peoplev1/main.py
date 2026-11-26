from typing import Optional

from fastapi import FastAPI
from pydantic import BaseModel, Field

app = FastAPI()


class Person(BaseModel):
    full_name: str = Field(alias="fullName")
    nick: Optional[str]


@app.post("/w4/v1/people")
def add_person(p: Person):
    return {"name": p.full_name}
