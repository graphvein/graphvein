class Item:
    def __init__(self, name: str):
        self.name = name
def label(item: Item) -> str:
    return item.name
