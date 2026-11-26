"""Module-name lists of the reference CPython for the Python structural
claim contract (docs/lang/python-structure.md, rows PS-I4/PS-I5, ADR 0069).

Prints, as JSON, what the running interpreter itself reports (nothing is
imported beyond what start-up loads; module specs are found with
`importlib.machinery.PathFinder` over the standard-library directory, which
reads the file system and runs no module code):

- `version`: the interpreter's full version;
- `builtin`: `sys.builtin_module_names`;
- `frozen`: the names of `_imp._frozen_module_names()` without a dot
  (`_imp.is_frozen` is true for each);
- `frozen_parents`: the first component of the dotted frozen names whose
  parent is not frozen itself (`importlib`: `importlib.machinery` is frozen,
  `importlib` is not);
- `startup`: the top-level names in `sys.modules` of a fresh `-I -S`
  interpreter (`__main__` excluded: it is the program itself);
- `meta_path`: builtin + frozen + startup: names the meta path or the module
  cache serves before any `sys.path` entry, so a repository file of the
  name is never loaded;
- `platform`: `sys.stdlib_module_names` entries that are not pure-Python
  modules in this interpreter's standard-library directory (extension
  modules, or modules of another platform) and not in `meta_path`: another
  build may compile them in, so whether a repository file of the name is
  loaded depends on the build; plus every other extension module of the
  interpreter's `lib-dynload` directory (CPython's test and example
  modules, which builds may compile in too) and `frozen_parents` (a
  repository package of the name is loaded, but its frozen submodules are
  not);
- `ascii_codecs`: the codec registry's names (normalised as
  `encodings.normalize_encoding` does: `encodings.aliases` keys and the
  `encodings` modules) of text codecs that decode the 128 ASCII bytes as
  ASCII; `utf8_codecs`: those naming the UTF-8 codec (row PS-F2,
  `encoding_cookie`);
- `pure`: the pure-Python top-level modules and packages of the
  standard-library directory (`.py` files, directories with `__init__.py`);
- `shipped`: top-level packages and modules in the standard-library
  directory that `sys.stdlib_module_names` leaves out (CPython's own `test`
  package), frozen names excluded.

usage: python3.14 -I stdlib_lists.py           (JSON)
       python3.14 -I stdlib_lists.py --go      (internal/lang/python/metapath.go)
"""

import _imp
import codecs
import encodings
import encodings.aliases
import importlib.machinery
import json
import os
import subprocess
import sys
import sysconfig
import warnings


def lists():
    lib = sysconfig.get_path("stdlib")
    builtin = sorted(sys.builtin_module_names)
    names = _imp._frozen_module_names()
    frozen = sorted(n for n in names if "." not in n)
    frozen_parents = sorted({n.split(".")[0] for n in names if "." in n} - set(frozen))
    out = subprocess.run([sys.executable, "-I", "-S", "-c",
                          "import sys; print(' '.join(sorted({m.split('.')[0] for m in sys.modules})))"],
                         check=True, capture_output=True, text=True).stdout.split()
    startup = sorted(set(out) - {"__main__"})
    meta = set(builtin) | set(frozen) | set(startup)
    platform = list(frozen_parents)
    dynload = os.path.join(sysconfig.get_path("platstdlib"), "lib-dynload")
    for entry in sorted(os.listdir(dynload)) if os.path.isdir(dynload) else []:
        name = entry.split(".")[0]
        if entry.endswith(tuple(importlib.machinery.EXTENSION_SUFFIXES)) and name not in meta and name not in platform:
            platform.append(name)
    for name in sorted(sys.stdlib_module_names):
        if name in platform:
            continue
        if name in frozen_parents:
            continue
        if name in meta:
            continue
        spec = importlib.machinery.PathFinder.find_spec(name, [lib])
        if spec is not None and (spec.origin is None or spec.origin.endswith(".py")):
            continue
        platform.append(name)
    pure = sorted(e[:-3] if e.endswith(".py") else e for e in os.listdir(lib)
                  if e.endswith(".py") or os.path.isfile(os.path.join(lib, e, "__init__.py")))
    shipped = []
    for entry in sorted(os.listdir(lib)):
        name = entry[:-3] if entry.endswith(".py") else entry
        if entry.endswith(".py") or (os.path.isdir(os.path.join(lib, entry)) and name.isidentifier() and name != "__pycache__"):
            if name.isidentifier() and name not in sys.stdlib_module_names and name not in meta:
                shipped.append(name)
    names = set(encodings.aliases.aliases) | set(encodings.aliases.aliases.values())
    for entry in os.listdir(os.path.dirname(encodings.__file__)):
        if entry.endswith(".py") and entry not in ("__init__.py", "aliases.py"):
            names.add(entry[:-3])
    ascii_codecs, utf8_codecs = [], []
    # ASCII text that stateful or escaping codecs read differently (escape
    # sequences, UTF-7 shifts, HZ and ISO-2022 shifts, IDNA labels).
    probes = [bytes(range(128)), b"\\n\\x41\\u0041\\N{DIGIT ONE}", b"+AGEA-", b"~{~}",
              b"\x1b$B\x1b(B", b"xn--bcher-kva.example"]
    for name in sorted(names):
        try:
            info = codecs.lookup(name)
            same = info._is_text_encoding and all(p.decode(name) == p.decode("ascii") for p in probes)
        except (LookupError, UnicodeDecodeError, TypeError, ValueError, DeprecationWarning):
            continue
        if same:
            ascii_codecs.append(name)
            if info.name == "utf-8":
                utf8_codecs.append(name)
    return {"version": "%d.%d.%d" % sys.version_info[:3], "builtin": builtin, "frozen": frozen,
            "frozen_parents": frozen_parents, "startup": startup, "meta_path": sorted(meta), "platform": sorted(platform), "pure": pure, "shipped": shipped,
            "ascii_codecs": ascii_codecs, "utf8_codecs": utf8_codecs}


GO = '''package python

// Code generated by scripts/oracle/python_structure/stdlib_lists.py --go
// from CPython %(version)s (the reference Python); DO NOT EDIT by hand.
//
// TestPythonStdlibNames checks these lists against the running interpreter:
// exactly when it is CPython %(version)s, and as inclusions otherwise (its
// meta-path names within metaPathNames and platformModuleNames).
// Rows PS-I4/PS-I5 of docs/lang/python-structure.md.

// metaPathNamesVersion is the CPython version the lists were read from.
const metaPathNamesVersion = "%(version)s"

// metaPathNames are the names the reference Python's meta path or module
// cache serves before any sys.path entry: sys.builtin_module_names, the
// frozen modules and the modules loaded at start-up (-I -S). An absolute
// import of one is python-stdlib even when the repository holds a file of
// the name.
const metaPathNames = `
%(meta_path)s
`

// platformModuleNames are standard-library names that are not pure-Python
// modules in the reference Python (extension modules or another platform's
// modules) and not meta-path names, so another build may compile them in,
// and the parents of frozen submodules (importlib): an import of one that
// the repository would supply claims nothing (platform_module).
const platformModuleNames = `
%(platform)s
`

// shippedModuleNames are top-level packages CPython installs in its
// standard-library directory that sys.stdlib_module_names leaves out (its
// own test package): python-stdlib names like the listed ones.
const shippedModuleNames = `
%(shipped)s
`

// asciiCodecNames are the reference Python's codec names (normalised as
// encodings.normalize_encoding) whose text codec reads ASCII bytes as
// ASCII; utf8CodecNames those of its UTF-8 codec (row PS-F2,
// encoding_cookie).
const asciiCodecNames = `
%(ascii_codecs)s
`

const utf8CodecNames = `
%(utf8_codecs)s
`
'''


def main():
    warnings.simplefilter("error", DeprecationWarning)
    data = lists()
    if "--go" in sys.argv:
        print(GO % {"version": data["version"], "meta_path": "\n".join(data["meta_path"]),
                    "platform": "\n".join(data["platform"]), "shipped": "\n".join(data["shipped"]),
                    "ascii_codecs": "\n".join(data["ascii_codecs"]), "utf8_codecs": "\n".join(data["utf8_codecs"])}, end="")
    else:
        print(json.dumps(data, indent=1))


if __name__ == "__main__":
    main()
