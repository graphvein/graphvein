from typing import Annotated

from fastapi import APIRouter, Depends, Header

from app.deps import pagination
from app.security import api_key


def tenant_header(x_tenant: Annotated[str, Header()]) -> str:
    return x_tenant


router = APIRouter(prefix="/admin/{tenant}", dependencies=[Depends(api_key)])


@router.get("/stats")
def stats(tenant: str, page: Annotated[dict, Depends(pagination)]):
    return {"tenant": tenant}


@router.get("/raw/{slot:int}")
def raw_slot():
    return {}
