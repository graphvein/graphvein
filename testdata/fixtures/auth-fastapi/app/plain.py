"""A dependency module that does not import fastapi (gate DG)."""


def plain_dependency(name: str):
    return name


def no_parameters():
    return 1
