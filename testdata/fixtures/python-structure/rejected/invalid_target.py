def ok():
    pass


del ok()
with ok() as (ok(), ok.attr):
    pass
