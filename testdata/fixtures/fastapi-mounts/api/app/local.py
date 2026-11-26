class Mount:
    """A project class that happens to be named Mount (not Starlette's)."""

    def __init__(self, path, app):
        self.path = path
        self.app = app
