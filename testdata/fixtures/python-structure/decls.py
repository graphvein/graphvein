from __future__ import annotations
from typing import overload
import typing


@overload
def conv(x: int) -> int: ...
@typing.overload
def conv(x: str,  # comment
         y: "a b" = 'q') -> str: ...
def conv(x, y=None):
    return x


@ app . route ( "/x" )
@property
def deco():
    pass


@decorator \
    .chained
def cont():
    pass


async def runner():
    async def inner():
        pass

    class Local:
        def m(self):
            def deep():
                pass
            return deep
    return inner


class Outer:
    if True:
        def cond_method(self):
            pass
    else:
        def cond_method(self):
            pass

    class Inner:
        def im(self):
            pass

    def body_comment(self):
        x = 1
        # trailing comment inside body
# comment at column zero after method


def redefined():
    pass


def redefined():
    pass


A = B = 1
X: int
C, D = 1, 2
*E, F = [1, 2]
G += 1 if False else 0
type Alias = int
def generic[T](x: T) -> T:
    return x


class GenericC[T]:
    pass


match = 1
type = 2
def match_fn(v):
    match v:
        case 1:
            def in_case():
                pass
    return None


ÄPFEL = 1
def naïve():
    pass
_PRIVATE = 3
lower = 4
MIXED_a = 5
if True:
    COND = 1
else:
    COND = 2
for LOOP in range(3):
    pass
def tabbed():
	return 1
