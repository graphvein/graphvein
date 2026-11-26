from fastapi import APIRouter as R

router = R(prefix="/items")
child = R()
orphan = R()
router.include_router(child, prefix="/nested")

@router.get("/{p:path}")
def item():
    pass

@child.api_route("/verbs", methods=["get", "POST", "get"])
def verbs():
    pass

@child.api_route(path=f"/default")
def default():
    pass

@orphan.get("/lost")
def lost():
    pass
