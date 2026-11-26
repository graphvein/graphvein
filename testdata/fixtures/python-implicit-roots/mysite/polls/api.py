from fastapi import APIRouter
from polls.models import PREFIX
from polls.views import view

router = APIRouter(prefix=PREFIX)
router.add_api_route('/list', view)
