type alias = int

try:
    pass
except* ValueError:
    from . import real as reexp
