# Row PS-M2: ws/src/wscore is a namespace directory on the ancestor root
# ws/src, and the workspace member ws/src/wscore installs the regular
# package ws/src/wscore/src/wscore, which PathFinder takes over it.
import wscore
from wscore import mod
# The installed wscore has no tests subpackage (ws/src/wscore/tests is
# beside the package, not in it): unresolved_in_repository.
from wscore.tests import helper
# The member ws/src/odd builds with setuptools: refused, so the namespace
# ws/src/odd still hides its package (namespace_hides_concrete).
import odd
# Two members install a package dupe: ambiguous_import.
import dupe
# A member matched by libs/* (hatchling's flat default layout).
import ws_glob
# An editable uv path source of the workspace root.
import wsext
# A uv path source no requirement names is not installed: the namespace
# wsundeclared/ on the root still hides its package.
import wsundeclared
