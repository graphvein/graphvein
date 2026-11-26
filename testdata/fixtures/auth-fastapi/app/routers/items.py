from typing import Annotated

from fastapi import APIRouter, Depends
from fastapi.security import HTTPBasic

from ..deps import AdminAlias, CurrentUser, FormDep, Pager, SessionDep, deep1, helper, later_unknown, optional_user, overridden, query_user
from ..plain import no_parameters, plain_dependency
from ..security import header_key, not_a_scheme, positional

router = APIRouter()


@router.get("/")
def read_items(session: SessionDep, user: CurrentUser):
    return []


@router.get("/admin")
def read_admin(user: AdminAlias):
    return user


@router.get("/optional")
def read_optional(credentials: Annotated[dict, Depends(optional_user)]):
    return credentials


@router.get("/query")
def read_query(key=Depends(query_user)):
    return key


@router.get("/listed", dependencies=[Depends(header_key)])
def read_listed():
    return []


@router.get("/positional", dependencies=[Depends(positional)])
def read_positional():
    return []


@router.post("/login")
def login(form: FormDep):
    return form


@router.get("/pages")
def read_pages(pager: Annotated[Pager, Depends()]):
    return pager


@router.get("/method")
def read_method(ok=Depends(helper.check)):
    return ok


@router.get("/inline")
def read_inline(credentials=Depends(HTTPBasic())):
    return credentials


@router.get("/plain")
def read_plain(value=Depends(plain_dependency), other=Depends(no_parameters)):
    return value


@router.get("/callable")
def read_callable(value=Depends(not_a_scheme)):
    return value


@router.get("/deep")
def read_deep(value=Depends(deep1)):
    return value


@router.get("/overridden")
def read_overridden(value=Depends(overridden)):
    return value


@router.get("/later")
def read_later(value=Depends(later_unknown)):
    return value


@router.get("/lambda")
def read_lambda(value=Depends(lambda: 1)):
    return value


@router.get("/open")
def read_open(limit: int = 10):
    return []
