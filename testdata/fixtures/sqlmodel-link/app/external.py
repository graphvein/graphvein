"""A base that does not resolve inside the repository: no table is claimed (R-B2, L2)."""

from typing import Optional

from sqlmodel import Field
from vendor_models import VendorBase


class Imported(VendorBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
