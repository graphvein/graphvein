def g(d):
    return f"{d["a"]}"


def h(*args: *tuple[int, ...]):
    pass


type L[T] = list[T]
try:
    pass
except* ValueError:
    pass


@(lambda f: f)
def lam():
    pass


@x[0].y
def sub():
    pass


def after():
    pass


class Q:
    def m(self):
        return f"{'''
multi'''}"

    def n(self):
        pass
