from fastapi import APIRouter

# Included twice: with dependencies on one chain only.
router = APIRouter()


@router.get("/shared")
def shared():
    return {}
