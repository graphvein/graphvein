"""Dependency functions and Annotated aliases (rows DA-F1, DA-F2)."""
from typing import Annotated

from fastapi import Depends, Query, Security
from fastapi.security import OAuth2PasswordRequestForm

from .security import cookie_bearer, fixed, oauth2, optional_bearer, query_key


def get_db():
    yield "db"


SessionDep = Annotated[str, Depends(get_db)]


def current_user(token: Annotated[str, Depends(oauth2)], db: SessionDep):
    return {"token": token, "db": db}


CurrentUser = Annotated[dict, Depends(current_user)]


def admin_user(user: CurrentUser):
    return user


def optional_user(credentials=Security(optional_bearer, scopes=["read"])):
    return credentials


def query_user(key: Annotated[str | None, Depends(query_key)] = None):
    return key


def cookie_user(token: Annotated[str | None, Depends(cookie_bearer)]):
    return token


def fixed_user(token: Annotated[str | None, Depends(fixed)]):
    return token


AdminAlias = Annotated[CurrentUser, Depends(admin_user)]


def marker():
    return "marker"


def overridden(value: Annotated[str, Depends(oauth2), Query()] = "x"):
    """Query() is the last FastAPI item: no dependency."""
    return value


def later_unknown(value: Annotated[str, Depends(oauth2), marker()]):
    """An unread call after Depends: unknown."""
    return value


class Pager:
    def __init__(self, skip: int = 0):
        self.skip = skip


class Helper:
    def check(self):
        return True


helper = Helper()

FormDep = Annotated[OAuth2PasswordRequestForm, Depends()]


# A chain of nine functions: the ninth step is beyond DA-F2's depth cap.
def deep9(token: Annotated[str, Depends(oauth2)]):
    return token


def deep8(x=Depends(deep9)):
    return x


def deep7(x=Depends(deep8)):
    return x


def deep6(x=Depends(deep7)):
    return x


def deep5(x=Depends(deep6)):
    return x


def deep4(x=Depends(deep5)):
    return x


def deep3(x=Depends(deep4)):
    return x


def deep2(x=Depends(deep3)):
    return x


def deep1(x=Depends(deep2)):
    return x
