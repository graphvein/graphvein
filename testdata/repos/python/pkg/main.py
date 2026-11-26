import typing
import pkg.util
import pkg.util as utility
from typing import overload
from .util import help
from . import util
from pkg import util as tools
from pkg.util import Helper as Imported
from ..missing import absent

VALUE = 1
private = 2

@typing.final
class User:
    @staticmethod
    def validate():
        return help()

    def save(self):
        return self.validate()

@overload
def convert(value: int) -> int: ...

@typing.overload
def convert(value: str = "a b") -> str: ...

def convert(value):
    return value

async def run():
    def validate():
        return help()
    class Local:
        pass
    validate()
    User()
    convert(1)
    pkg.util.help()
    utility.help()
    util.help()
    tools.help()
    User.validate()
    util.Helper.method()
    Imported.method()
    print("external")
    missing()

if VALUE:
    def conditional():
        return help()
else:
    def conditional():
        return help()
