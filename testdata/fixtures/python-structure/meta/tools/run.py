# meta/tools is an implicit root (tools/rpkg is a top-level package there).
# The explicit root meta/ answers rpkg first under the policy, while
# `python meta/tools/run.py` would search this directory first: refused.
import rpkg

# Only this directory holds toolonly: both orders agree, the edge is claimed.
import toolonly
