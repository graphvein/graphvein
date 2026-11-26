from fastapi import APIRouter, Depends

from ..deps import cookie_user, fixed_user
from ..security import cookie_key, digest

# Router-level dependencies.
router = APIRouter(dependencies=[Depends(cookie_key)])


@router.get("/stats")
def stats():
    return {}


@router.get("/cookie")
def cookie(user=Depends(cookie_user)):
    return user


@router.get("/fixed", dependencies=[Depends(fixed_user), Depends(digest)])
def fixed_route():
    return {}
