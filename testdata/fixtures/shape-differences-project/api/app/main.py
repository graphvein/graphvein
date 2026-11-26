from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI()


class UserCreate(BaseModel):
    email: str
    name: str


# SD-11: the web member's call sends `role`, which UserCreate does not
# declare, and leaves out `name`, which it requires.
@app.post("/w4p/users")
def create_user(user: UserCreate):
    return {"email": user.email}
