"""Revision 1 case: Decimal without max_digits is Numeric without precision (F5)."""

import decimal

from typing import Optional

from sqlmodel import Field, SQLModel


class Priced(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    price: decimal.Decimal
