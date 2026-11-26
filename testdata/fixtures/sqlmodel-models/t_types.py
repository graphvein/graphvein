"""Explicit type expressions in sa_type: row T1 (T-list classes and refused arguments)."""

import enum
from typing import Optional

from sqlalchemy import DateTime, Enum, Numeric, String
from sqlmodel import Field, SQLModel
from sqlmodel.sql.sqltypes import AutoString

LENGTH = 9


class Kind(enum.Enum):
    a = "a"
    b = "b"


class Types(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    short: str = Field(sa_type=String(64))
    named_length: str = Field(sa_type=String(length=32))
    auto: str = Field(sa_type=AutoString)
    stamp: str = Field(sa_type=DateTime(timezone=True))
    kind: Kind = Field(sa_type=Enum(Kind))
    money: float = Field(sa_type=Numeric(10, 2, asdecimal=False))
    unknown_length: str = Field(sa_type=String(LENGTH))



# Oracle cases for every claimed type form (T018g0 Revision 1): T1's bare
# Numeric (precision and scale "none") and Numeric(10, 2) (arguments:
# unknown), F5's Decimal annotation, then every other T1 class and every F5
# mapping once. Imported here so the lines above keep their numbers in the
# goldens.
import datetime  # noqa: E402
import decimal  # noqa: E402
import uuid  # noqa: E402

from sqlalchemy import (  # noqa: E402
    JSON,
    BigInteger,
    Boolean,
    Date,
    Float,
    Integer,
    Interval,
    LargeBinary,
    SmallInteger,
    Text,
    Time,
    Unicode,
    UnicodeText,
    Uuid,
)


class Numerics(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    bare: decimal.Decimal = Field(sa_type=Numeric)
    sized: decimal.Decimal = Field(sa_type=Numeric(10, 2))
    plain: decimal.Decimal


class TypeClasses(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    t_uuid: uuid.UUID = Field(sa_type=Uuid)
    t_text: str = Field(sa_type=Text)
    t_unicode: str = Field(sa_type=Unicode)
    t_unicode_text: str = Field(sa_type=UnicodeText)
    t_integer: int = Field(sa_type=Integer)
    t_big: int = Field(sa_type=BigInteger)
    t_small: int = Field(sa_type=SmallInteger)
    t_boolean: bool = Field(sa_type=Boolean)
    t_datetime: datetime.datetime = Field(sa_type=DateTime)
    t_date: datetime.date = Field(sa_type=Date)
    t_time: datetime.time = Field(sa_type=Time)
    t_interval: datetime.timedelta = Field(sa_type=Interval)
    t_float: float = Field(sa_type=Float)
    t_json: str = Field(sa_type=JSON)
    t_binary: bytes = Field(sa_type=LargeBinary)


# F5 source types that map to the same class are separate oracle forms
# (T018g0 Revision 2): AutoString's other sources and Enum's other bases.
import ipaddress  # noqa: E402
import pathlib  # noqa: E402

from pydantic import EmailStr  # noqa: E402


class StrKind(enum.StrEnum):
    a = "a"


class IntKind(enum.IntEnum):
    a = 1


class FlagKind(enum.Flag):
    a = 1


class IntFlagKind(enum.IntFlag):
    a = 1


class MappedTypes(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    m_str: str
    m_float: float
    m_bool: bool
    m_datetime: datetime.datetime
    m_date: datetime.date
    m_timedelta: datetime.timedelta
    m_time: datetime.time
    m_bytes: bytes
    m_uuid: uuid.UUID
    m_enum: Kind
    m_email: EmailStr
    m_path: pathlib.Path
    m_ipv4: ipaddress.IPv4Address
    m_ipv4_net: ipaddress.IPv4Network
    m_ipv6: ipaddress.IPv6Address
    m_ipv6_net: ipaddress.IPv6Network
    m_str_enum: StrKind
    m_int_enum: IntKind
    m_flag: FlagKind
    m_int_flag: IntFlagKind
