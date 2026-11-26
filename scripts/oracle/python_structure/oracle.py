"""Python structure oracle (T020: an investigation prototype, then the claim contract).

Compares what Graphvein emits for Python files (file records, declaration
nodes, `contains` edges, `imports` edges) with Python's own toolchain over
the raw bytes of each file:

- declarations, names and spans from `ast` (the CPython parser of the
  running interpreter), mapped to contract spans (1-based lines, 0-based
  byte columns in the file's own encoding, end exclusive) with the line
  splitting of the CPython tokenizer (LF, CRLF and lone CR);
- import resolution re-derived from the import system's rules
  (`importlib.machinery.PathFinder` / `FileFinder` semantics, stepwise per
  dotted component, regular and namespace packages, loader suffix order
  from `importlib.machinery`, relative imports with `__package__` levels,
  `from`-list submodule handling) over a directory listing of the
  checkout. No corpus module is imported or executed: `find_spec` on a
  dotted name imports its parents, so it is never called.

What the oracle takes from Graphvein's policy (docs/lang/python-resolution.md)
rather than from Python: the search-path root set and its order (explicit
roots, `src` children, implicit roots, sibling fallback); there is no
interpreter `sys.path` for a repository. Declaration identities follow
docs/lang/identity.md ("Python"); the oracle derives them from `ast`, not
from tree-sitter.

The oracle also applies the claim contract (docs/lang/python-structure.md) on top of
what Python itself does: a file the reference Python rejects claims nothing,
annotation-only and starred names are no constants, an import Python would
satisfy from a file Graphvein does not read, a relative import beyond the
top-level package, a self edge, an undecided from-name and an in-repository
top-level name claim no edge; unresolved standard-library names are
`python-stdlib` externals (this interpreter's sys.stdlib_module_names).
Every record carries `gate`: true for a claimed fact that disagrees (a wrong,
extra or missing claim), false for an omission the contract explains or a
note. TestPythonStructureOracle fails on any gate record.

Run with `python3.14 -I` (the reference Python; no site, no environment, no
current directory on sys.path); it refuses an older interpreter.

usage: python3 -I oracle.py --snapshot S.ndjson --root CHECKOUT
           --corpus NAME --out OUT.jsonl [--summary SUMMARY.json]
"""

import argparse
import ast
import collections
import fnmatch
import hashlib
import importlib.machinery
import io
import json
import os
import re
import site
import subprocess
import sys
import sysconfig
import tokenize
import tomllib
import unicodedata

# Loader suffix order of FileFinder (extensions, source, bytecode), read
# from the running interpreter, not written down by hand.
SUFFIXES = (list(importlib.machinery.EXTENSION_SUFFIXES)
            + list(importlib.machinery.SOURCE_SUFFIXES)
            + list(importlib.machinery.BYTECODE_SUFFIXES))

# The meta path and module cache of the running interpreter:
# what CPython serves before any sys.path entry. Built-in and frozen
# modules come from its own importers (BuiltinImporter, FrozenImporter
# find_spec reads tables, runs no module code); start-up modules from a
# fresh `-I -S` interpreter's sys.modules. `__main__` is the running program
# and site imports `sitecustomize` / `usercustomize` before the script
# directory is on sys.path: modules the repository cannot show.
STARTUP = set(subprocess.run([sys.executable, "-I", "-S", "-c", "import sys; print(' '.join(sys.modules))"],
                             check=True, capture_output=True, text=True).stdout.split())
INTERPRETER = {"__main__"} | {n for n, hook in (("sitecustomize", "execsitecustomize"), ("usercustomize", "execusercustomize"))
                              if hasattr(site, hook)}
# The standard library's own sys.path entries (no site-packages).
STDLIB_PATHS = []
for _p in [sysconfig.get_path("stdlib"), sysconfig.get_path("platstdlib")] + [p for p in sys.path if p.endswith("lib-dynload")]:
    if _p and _p not in STDLIB_PATHS:
        STDLIB_PATHS.append(_p)


def builtin_served(top):
    return importlib.machinery.BuiltinImporter.find_spec(top) is not None


def meta_served(top):
    """Frozen or loaded at start-up: served before sys.path on every run."""
    return importlib.machinery.FrozenImporter.find_spec(top) is not None or top in STARTUP


def stdlib_spec(top):
    """CPython's own PathFinder over the standard library's entries."""
    return importlib.machinery.PathFinder.find_spec(top, STDLIB_PATHS)


def stdlib_pure(top):
    spec = importlib.machinery.PathFinder.find_spec(top, [sysconfig.get_path("stdlib")])
    return spec is not None and (spec.origin is None or spec.origin.endswith(".py"))


def stdlib_name(top):
    spec = stdlib_spec(top)
    return top in sys.stdlib_module_names or (spec is not None and spec.origin is not None and spec.origin.endswith(".py"))


DECL_KINDS = ("function", "method", "class", "constant")

# The reference Python of the contract (docs/lang/python-structure.md).
REFERENCE = (3, 14)

# Record classes that are a disagreement on a claimed fact.
GATE = {"F-hash", "F-size", "D-claimed-in-rejected-file", "D-missing", "D-extra", "D-differs", "D-oracle-error",
        "I-wrong-edge", "I-claimed-undecided", "I-external-differs", "I-missing-edge", "I-external-missing",
        "I-span-unmatched", "I-claimed-in-rejected-file", "I-relative-beyond-top"}


def is_gate(cls):
    return cls in GATE or cls.startswith("D-span-")

# Import forms worth listing per site even when both sides agree.
NOTABLE = {"external_but_top_in_repo", "relative_beyond_top_under_some_root", "ambiguous",
           "namespace_module", "target_not_scanned"}


# --------------------------------------------------------------------------
# Snapshot


class Snapshot:
    def __init__(self, path):
        self.files = {}        # path -> file record (all languages)
        self.nodes = {}        # identity -> node
        self.edges = []
        self.diagnostics = collections.defaultdict(list)
        with open(path, "rb") as f:
            for line in f:
                r = json.loads(line)
                t = r.get("type")
                if t == "file":
                    self.files[r["path"]] = r
                elif t == "node":
                    self.nodes[r["identity"]] = r
                elif t == "edge":
                    if r["kind"] in ("contains", "imports"):
                        self.edges.append(r)
                elif t == "diagnostic":
                    self.diagnostics[r.get("file", "")].append(r)

    def python_files(self):
        return sorted(p for p, r in self.files.items() if r.get("language") == "python")


# --------------------------------------------------------------------------
# Source text as CPython reads it


def sha(text_bytes):
    s = text_bytes.replace(b"\r\n", b"\n").replace(b"\r", b"\n")
    return "sha256:" + hashlib.sha256(s).hexdigest()


class Source:
    """Raw bytes plus the CPython line model.

    Lines end at LF, CRLF or a lone CR (the tokenizer translates all three).
    `ast` columns are UTF-8 byte offsets into the decoded line; contract
    columns are byte offsets into the raw line in the file's encoding, so a
    position is mapped through the decoded line text.
    """

    def __init__(self, raw):
        self.raw = raw
        self.encoding, _ = tokenize.detect_encoding(io.BytesIO(raw).readline)
        self.bom = raw.startswith(b"\xef\xbb\xbf")
        # raw line starts (byte offsets) with CPython's line ends
        self.starts = [0]
        i, n = 0, len(raw)
        while i < n:
            c = raw[i]
            if c == 0x0A:
                self.starts.append(i + 1)
            elif c == 0x0D:
                if i + 1 < n and raw[i + 1] == 0x0A:
                    i += 1
                self.starts.append(i + 1)
            i += 1
        self._decoded = {}

    def raw_line(self, line):  # 1-based, without its line end
        a = self.starts[line - 1]
        b = self.starts[line] if line < len(self.starts) else len(self.raw)
        s = self.raw[a:b]
        return s.rstrip(b"\n").rstrip(b"\r") if s.endswith((b"\n", b"\r")) else s

    def text_line(self, line):
        if line not in self._decoded:
            rl = self.raw_line(line)
            if line == 1 and self.bom:
                rl = rl[3:]
            enc = "utf-8" if self.encoding in ("utf-8-sig",) else self.encoding
            self._decoded[line] = rl.decode(enc)
        return self._decoded[line]

    def char_col(self, line, utf8_col):
        """ast column (UTF-8 bytes of the decoded line) -> character index."""
        t = self.text_line(line)
        return len(t.encode("utf-8")[:utf8_col].decode("utf-8", "strict"))

    def byte_col(self, line, char_col):
        """character index -> raw byte column (file encoding, BOM counted)."""
        t = self.text_line(line)
        enc = "utf-8" if self.encoding in ("utf-8-sig",) else self.encoding
        b = len(t[:char_col].encode(enc))
        if line == 1 and self.bom:
            b += 3
        return b

    def pos(self, line, utf8_col):
        return line, self.byte_col(line, self.char_col(line, utf8_col))

    def offset(self, line, byte_col):
        return self.starts[line - 1] + byte_col


# --------------------------------------------------------------------------
# Declarations (identity.md "Python") from ast


def upper(name):
    letter = False
    for ch in name:
        if ch.isalpha():
            letter = True
            if not ch.isupper():
                return False
        elif ch != "_" and unicodedata.category(ch) != "Nd":
            # Go's unicode.IsDigit (Nd), not str.isdigit, which also takes
            # No digits such as U+1369 ETHIOPIC DIGIT ONE.
            return False
    return letter


def target_names(t, starred=True):
    """Names a target binds; with starred=False, not those under `*`
    (row PS-D7 claims no starred name)."""
    if isinstance(t, ast.Name):
        return [t]
    if isinstance(t, (ast.Tuple, ast.List)):
        out = []
        for e in t.elts:
            out.extend(target_names(e, starred))
        return out
    if isinstance(t, ast.Starred) and starred:
        return target_names(t.value)
    return []


def compact_params(src, text_lines, fn):
    """Parameter text between the def's parentheses, trivia removed, from
    CPython's own tokenizer (comments, NL and whitespace dropped; every
    token kept as written)."""
    # decoded text from the def line to the end of the def's header
    start_line = fn.lineno
    end_line = fn.body[0].lineno if fn.body else fn.end_lineno
    lines = [text_lines(l) + "\n" for l in range(start_line, end_line + 1)]
    out = []
    try:
        gen = tokenize.generate_tokens(io.StringIO("".join(lines)).readline)
        depth = 0
        seen_name = False
        brackets = 0
        started = False
        for tok in gen:
            if not started:
                if tok.type == tokenize.NAME and tok.string == fn.name and not seen_name:
                    seen_name = True
                    continue
                if seen_name and tok.type == tokenize.OP and tok.string == "[" and brackets == 0 and depth == 0:
                    brackets = 1
                    continue
                if brackets:
                    if tok.string == "[":
                        brackets += 1
                    elif tok.string == "]":
                        brackets -= 1
                    continue
                if seen_name and tok.type == tokenize.OP and tok.string == "(":
                    started = True
                    depth = 1
                    out.append("(")
                continue
            if tok.type in (tokenize.COMMENT, tokenize.NL, tokenize.NEWLINE, tokenize.INDENT, tokenize.DEDENT):
                continue
            if tok.type == tokenize.OP and tok.string == "(":
                depth += 1
            elif tok.type == tokenize.OP and tok.string == ")":
                depth -= 1
            out.append(tok.string)
            if depth == 0:
                break
    except (tokenize.TokenError, SyntaxError):
        return None
    return "".join(out)


def decorator_name(src, d):
    """The decorator as written after '@' (a call: its callee), with the
    parentheses ast leaves out of a parenthesised expression."""
    e = d.func if isinstance(d, ast.Call) else d
    text = src.decoded_text
    lines = text.split("\n")
    # character offsets of e in the decoded text
    def off(line, col_utf8):
        return sum(len(l) + 1 for l in lines[:line - 1]) + len(lines[line - 1].encode("utf-8")[:col_utf8].decode("utf-8"))
    a = off(e.lineno, e.col_offset)
    b = off(e.end_lineno, e.end_col_offset)
    opened = 0
    i = a - 1
    while i >= 0 and text[i] in " \t\f(\\\n":
        if text[i] == "(":
            opened += 1
            a = i
        i -= 1
    while opened:
        j = b
        while j < len(text) and text[j] in " \t\f\\\n":
            j += 1
        if j < len(text) and text[j] == ")":
            b = j + 1
            opened -= 1
        else:
            break
    return text[a:b]


class DeclOracle:
    def __init__(self, path, src, tree):
        self.path = path
        self.src = src
        self.tree = tree
        self.counts = collections.Counter()
        self.decls = []  # dicts
        self.forms = collections.Counter()

    def span_of(self, start_line, start_col_utf8, end_line, end_col_utf8):
        sl, sc = self.src.pos(start_line, start_col_utf8)
        el, ec = self.src.pos(end_line, end_col_utf8)
        return sl, sc, el, ec

    def at_sign(self, d):
        """Position of the '@' of decorator expression d: the token before the
        expression, skipping white space and backslash continuations."""
        line, col = d.lineno, self.src.char_col(d.lineno, d.col_offset)
        while True:
            t = self.src.text_line(line)
            j = col - 1
            while j >= 0 and t[j] in " \t\f(":
                j -= 1
            if j >= 0:
                if t[j] == "@":
                    return line, j
                return None
            # continuation from the previous line ends with a backslash
            line -= 1
            prev = self.src.text_line(line)
            if not prev.endswith("\\"):
                return None
            col = len(prev) - 1

    def add(self, kind, name, q, owner, span, extra=None):
        key = q
        self.counts[key] += 1
        if self.counts[key] > 1:
            q = q + "@" + str(self.counts[key])
        sl, sc, el, ec = span
        a = self.src.offset(sl, sc)
        b = self.src.offset(el, ec)
        d = {"kind": kind, "file": self.path, "q": q, "name": name, "owner": owner,
             "span": [sl, sc, el, ec], "hash": sha(self.src.raw[a:b])}
        if extra:
            d.update(extra)
        self.decls.append(d)
        return q

    def run(self):
        self.body(self.tree.body, "", None, False)
        return self.decls

    def body(self, stmts, scope, owner, in_class):
        for s in stmts:
            self.stmt(s, scope, owner, in_class)

    def stmt(self, s, scope, owner, in_class):
        if isinstance(s, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            if isinstance(s, ast.ClassDef):
                kind = "class"
            elif in_class:
                kind = "method"
            else:
                kind = "function"
            if isinstance(s, ast.AsyncFunctionDef):
                self.forms["async_def"] += 1
            if s.decorator_list:
                self.forms["decorated"] += 1
            if owner is not None:
                self.forms["nested_" + kind] += 1
            if getattr(s, "type_params", None):
                self.forms["type_params"] += 1
            q = scope + "." + s.name if scope else s.name
            decorators = [decorator_name(self.src, d) for d in s.decorator_list]
            overload = any(n in ("overload", "typing.overload") for n in decorators)
            if overload and kind != "class":
                self.forms["overload"] += 1
                params = compact_params(self.src, self.src.text_line, s)
                q = q + "@overload" + (params if params is not None else "<?>")
            if s.decorator_list:
                at = self.at_sign(s.decorator_list[0])
                if at is None:
                    sl, sc = self.src.pos(s.lineno, s.col_offset)
                    self.forms["decorator_at_not_found"] += 1
                else:
                    sl, sc = at[0], self.src.byte_col(at[0], at[1])
            else:
                sl, sc = self.src.pos(s.lineno, s.col_offset)
            el, ec = self.src.pos(s.end_lineno, s.end_col_offset)
            extra = {"decorators": decorators} if decorators else {"decorators": None}
            q = self.add(kind, s.name, q, owner, (sl, sc, el, ec), extra)
            me = (kind, q)
            self.body(s.body, q, me, kind == "class")
            return
        if isinstance(s, (ast.Assign, ast.AnnAssign)) and scope == "":
            targets = s.targets if isinstance(s, ast.Assign) else [s.target]
            if isinstance(s, ast.AnnAssign) and s.value is None:
                # Row PS-D7: an annotation without a value is no constant.
                self.forms["annotation_only"] += 1
                return
            for i, t in enumerate(targets):
                for nm in target_names(t, starred=False):
                    if upper(nm.id):
                        # span: from this target to the end of the statement
                        # (tree-sitter nests chained assignments)
                        sl, sc = self.src.pos(t.lineno, t.col_offset) if i > 0 else self.src.pos(s.lineno, s.col_offset)
                        el, ec = self.src.pos(s.end_lineno, s.end_col_offset)
                        if i > 0:
                            self.forms["chained_constant"] += 1
                        if isinstance(t, (ast.Tuple, ast.List)):
                            self.forms["unpacked_constant"] += 1
                        self.add("constant", nm.id, nm.id, None, (sl, sc, el, ec), {"decorators": None})
                if isinstance(t, (ast.Tuple, ast.List)):
                    for e in ast.walk(t):
                        if isinstance(e, ast.Starred) and isinstance(e.value, ast.Name) and upper(e.value.id):
                            self.forms["starred_upper_target"] += 1
            return
        if isinstance(s, ast.TypeAlias):
            self.forms["type_alias_statement"] += 1
            return
        if isinstance(s, ast.AugAssign) and scope == "" and isinstance(s.target, ast.Name) and upper(s.target.id):
            self.forms["augassign_upper"] += 1
        # compound statements do not open a lexical scope
        if isinstance(s, ast.If):
            if scope == "" and is_type_checking(s.test):
                self.forms["if_type_checking"] += 1
            self.body(s.body, scope, owner, in_class)
            self.body(s.orelse, scope, owner, in_class)
        elif isinstance(s, (ast.For, ast.AsyncFor, ast.While)):
            self.body(s.body, scope, owner, in_class)
            self.body(s.orelse, scope, owner, in_class)
        elif isinstance(s, (ast.With, ast.AsyncWith)):
            self.body(s.body, scope, owner, in_class)
        elif isinstance(s, (ast.Try, ast.TryStar)):
            self.body(s.body, scope, owner, in_class)
            for h in s.handlers:
                self.body(h.body, scope, owner, in_class)
            self.body(s.orelse, scope, owner, in_class)
            self.body(s.finalbody, scope, owner, in_class)
        elif isinstance(s, ast.Match):
            self.forms["match"] += 1
            for c in s.cases:
                self.body(c.body, scope, owner, in_class)


def is_type_checking(test):
    return (isinstance(test, ast.Name) and test.id == "TYPE_CHECKING") or (
        isinstance(test, ast.Attribute) and test.attr == "TYPE_CHECKING")


# --------------------------------------------------------------------------
# Import system (PathFinder / FileFinder semantics over a directory listing)


class Disk:
    """Directory listing of the checkout (what FileFinder would see). Only
    `.git` is skipped; the scanner's own ignore rules are not applied.
    Symlinked directories inside the checkout are followed, as FileFinder
    follows them (a link back to an ancestor is not entered)."""

    def __init__(self, root):
        self.root = root
        self.dirs = set()
        self.files = set()
        top = os.path.realpath(root)

        def inside(p):
            real = os.path.realpath(p)
            return real == top or real.startswith(top + os.sep)

        for d, subdirs, files in os.walk(root, followlinks=True):
            real = os.path.realpath(d)
            if real != os.path.normpath(os.path.join(top, os.path.relpath(d, root))):
                # Under a symlink: stop at a cycle (a link to an ancestor).
                up, cycle = os.path.dirname(d), False
                while len(up) >= len(root):
                    if os.path.realpath(up) == real:
                        cycle = True
                        break
                    up = os.path.dirname(up)
                if cycle:
                    subdirs[:] = []
                    continue
            # Links out of the checkout are not followed (nothing outside
            # the repository is listed).
            subdirs[:] = [s for s in subdirs if s != ".git" and inside(os.path.join(d, s))]
            rel = os.path.relpath(d, root).replace(os.sep, "/")
            self.dirs.add(rel)
            for f in files:
                p = f if rel == "." else rel + "/" + f
                if os.path.isfile(os.path.join(d, f)):
                    self.files.add(p)

    @staticmethod
    def join(a, b):
        return b if a == "." else a + "/" + b

    def isdir(self, p):
        return p in self.dirs

    def isfile(self, p):
        return p in self.files


class Spec:
    def __init__(self, name, origin, locations):
        self.name = name
        self.origin = origin          # file path or None (namespace)
        self.locations = locations    # list of dirs or None (module)

    def concrete(self):
        return self.origin is not None


class SiteEntry:
    """The installed packages of the declared environment (row PS-M2), as
    one site-packages directory: it holds exactly the members' declared
    packages (name -> package directories). Finding a name there is
    FileFinder over the package's parent directory, restricted to that
    name: a regular install copies the package there, an editable one puts
    the parent on sys.path; for the package's own name both give this
    answer. Two members installing one name: the environment is not
    determined (Ambiguous)."""

    def __init__(self, packages):
        self.packages = packages


class Ambiguous(Exception):
    def __init__(self, inits):
        self.inits = inits


def file_finder(disk, entry, tail):
    """importlib's FileFinder.find_spec for one path entry and one name."""
    if isinstance(entry, SiteEntry):
        dirs = entry.packages.get(tail)
        if not dirs:
            return None
        if tail in sys.stdlib_module_names or builtin_served(tail) or meta_served(tail) or stdlib_spec(tail) is not None:
            # site-packages comes after the standard library: CPython
            # takes the standard module (this interpreter's own lists and
            # PathFinder; another platform's names count as standard too).
            return None
        if len(dirs) > 1:
            raise Ambiguous(sorted(d + "/__init__.py" for d in dirs))
        return file_finder(disk, os.path.dirname(dirs[0]) or ".", tail)
    base = disk.join(entry, tail)
    is_namespace = False
    if disk.isdir(base):
        for suf in SUFFIXES:
            init = base + "/__init__" + suf
            if disk.isfile(init):
                return Spec(tail, init, [base])
        is_namespace = True
    for suf in SUFFIXES:
        full = disk.join(entry, tail + suf)
        if disk.isfile(full):
            return Spec(tail, full, None)
    if is_namespace:
        return Spec(tail, None, [base])
    return None


def path_finder(disk, tail, path):
    """importlib's PathFinder._get_spec: first concrete spec wins; namespace
    portions accumulate over every entry."""
    portions = []
    for entry in path:
        spec = file_finder(disk, entry, tail)
        if spec is None:
            continue
        if spec.concrete():
            return spec
        portions.extend(spec.locations)
    if portions:
        return Spec(tail, None, portions)
    return None


def find_dotted(disk, parts, path):
    """Stepwise import of a.b.c: each component searched in the parent's
    __path__. Returns (spec, error) where error names a refusal."""
    spec = None
    for i, p in enumerate(parts):
        if i > 0:
            if spec.locations is None:
                return None, "parent_not_package"
            path = spec.locations
        spec = path_finder(disk, p, path)
        if spec is None:
            return None, "not_found" if i == 0 else "submodule_not_found"
    return spec, None


# --------------------------------------------------------------------------
# Resolution policy taken from Graphvein (python-resolution.md)


class Roots:
    def __init__(self, snap, py_files):
        explicit = {"."}
        for p in snap.files:
            if os.path.basename(p) in ("pyproject.toml", "setup.py", "setup.cfg"):
                explicit.add(os.path.dirname(p) or ".")
        pydirs = set()
        for f in py_files:
            d = os.path.dirname(f) or "."
            while True:
                pydirs.add(d)
                if d == ".":
                    break
                d = os.path.dirname(d) or "."
        for r in list(explicit):
            src = "src" if r == "." else r + "/src"
            if src in pydirs:
                explicit.add(src)
        self.explicit = sorted(explicit)
        pyset = set(py_files)
        implicit = set()
        for f in py_files:
            if os.path.basename(f) != "__init__.py":
                continue
            d = os.path.dirname(f) or "."
            parent = os.path.dirname(d) or "." if d != "." else "."
            init = "__init__.py" if parent == "." else parent + "/__init__.py"
            if init not in pyset and parent not in explicit:
                implicit.add(parent)
        self.implicit = sorted(implicit, key=lambda r: (-len(r), r))

    @staticmethod
    def ancestor(root, f):
        return root == "." or f == root or f.startswith(root + "/")

    def site_for(self, importer):
        return self.env.site_for(importer)


# --------------------------------------------------------------------------
# Declared environments (row PS-M2), read with tomllib from the raw
# pyproject.toml files on disk (the whole listing, not the scanner's
# inventory), with this file's own reading of uv 0.13.9's discovery
# (crates/uv-workspace/src/workspace.rs: Workspace::discover,
# find_workspace, collect_members_only, WorkspaceExclusions) and of the
# glob crate 0.3.4 (glob() walks one component at a time; exclusions use
# Pattern::matches_path with the default MatchOptions, so `*` and `?`
# cross `/`: fnmatch.fnmatchcase has the same meaning for `*`, `?` and
# literals). The packages each installed project exposes follow
# hatchling 1.31.0's wheel options and default file selection and its
# editable build (the .pth holds the parent of each exposed package). The
# contract's refusals (other backends and hatch options, a hatch.toml,
# glob syntax other than `*` and `?`, non-editable path sources) are
# applied as policy.


def _join(base, p):
    if not p or p.startswith("/") or "\\" in p or ":" in p:
        return None
    j = os.path.normpath(os.path.join(base, p)).replace(os.sep, "/")
    if j == ".." or j.startswith("../"):
        return None
    return j


def _strings(v):
    return v if isinstance(v, list) and all(isinstance(x, str) and x for x in v) else None


def _is_identifier(n):
    return n.isascii() and n.isidentifier()


def _pep503(n):
    return re.sub(r"[-_.]+", "-", n).lower()


def _requirements(v):
    """Unconditional requirement names (no marker, no direct reference)."""
    out = []
    for item in v if isinstance(v, list) else []:
        if not isinstance(item, str):
            continue
        m = re.match(r"\s*([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(.*)$", item)
        if m and ";" not in m.group(2) and "@" not in m.group(2):
            out.append(_pep503(m.group(1)))
    return out


def _uv(doc):
    t = doc.get("tool", {})
    return t.get("uv", {}) if isinstance(t, dict) else {}


class Environments:
    """uv's view of the checkout: for an importer, the packages its
    environment has installed (name -> package directories)."""

    def __init__(self, disk):
        self.disk = disk
        self.docs, self.broken = {}, set()
        for f in disk.files:
            if os.path.basename(f) == "pyproject.toml":
                d = os.path.dirname(f) or "."
                try:
                    with open(os.path.join(disk.root, f), "rb") as h:
                        self.docs[d] = tomllib.load(h)
                except (tomllib.TOMLDecodeError, UnicodeDecodeError, OSError):
                    self.broken.add(d)
        self.cache = {}
        self.children = collections.defaultdict(list)
        for d in disk.dirs:
            if d != ".":
                self.children[os.path.dirname(d) or "."].append(d)

    def has_pyproject(self, d):
        return d in self.docs or d in self.broken

    def site_for(self, importer):
        d = os.path.dirname(importer) or "."
        while True:
            if self.has_pyproject(d):
                if d not in self.cache:
                    self.cache[d] = self.environment(d)
                return self.cache[d]
            if d == ".":
                return {}
            d = os.path.dirname(d) or "."

    def environment(self, d):
        doc = self.docs.get(d)
        if doc is None or _uv(doc).get("managed") is False:
            return {}
        if "workspace" in _uv(doc):
            return self.workspace_env(d)
        up = d
        while up != ".":
            up = os.path.dirname(up) or "."
            if self.has_pyproject(up):
                if up in self.broken:
                    return {}
                if "workspace" in _uv(self.docs[up]):
                    members = self.members(up)
                    if members is None:
                        return {}
                    if d in members:
                        return self.workspace_env(up)
                return self.standalone_env(d)
        return self.standalone_env(d)

    def glob(self, root, pattern):
        current = [root]
        for comp in [] if pattern == "." else pattern.split("/"):
            nxt = []
            for c in current:
                if "*" in comp or "?" in comp:
                    nxt.extend(x for x in self.children[c] if fnmatch.fnmatchcase(x.rsplit("/", 1)[-1], comp))
                else:
                    p = comp if c == "." else c + "/" + comp
                    if self.disk.isdir(p):
                        nxt.append(p)
            current = nxt
        return current

    def members(self, root):
        ws = _uv(self.docs[root]).get("workspace")
        if not isinstance(ws, dict) or any(k not in ("members", "exclude") for k in ws):
            return None
        if _uv(self.docs[root]).get("managed") is False:
            return None
        pats = {}
        for key in ("members", "exclude"):
            v = ws.get(key, [])
            if not isinstance(v, list) or not all(isinstance(x, str) for x in v):
                return None
            for x in v:
                if any(c in x for c in "[]\\") or "**" in x:
                    return None
            pats[key] = [os.path.normpath(x).replace(os.sep, "/") for x in v]
        if any(x == ".." or x.startswith("../") or x.startswith("/") for x in pats["members"] + pats["exclude"]):
            return None
        out = {root} if isinstance(self.docs[root].get("project"), dict) else set()

        def rel(x):
            return x if root == "." else x[len(root) + 1:]

        for pat in pats["members"]:
            for m in self.glob(root, pat):
                if m == root or m in out or any(fnmatch.fnmatchcase(rel(m), e) for e in pats["exclude"]):
                    continue
                if self.has_pyproject(m):
                    out.add(m)
                    continue
                base = m.rsplit("/", 1)[-1]
                has_files = any(f.startswith(m + "/") for f in self.disk.files)
                if base.startswith(".") or not has_files:
                    continue
                return None
        result, names = [], set()
        for m in sorted(out):
            if m in self.broken:
                return None
            doc = self.docs[m]
            if m != root:
                if _uv(doc).get("managed") is False:
                    continue
                if "workspace" in _uv(doc):
                    return None
            project = doc.get("project")
            if not isinstance(project, dict):
                return None
            key = _pep503(str(project.get("name", "")))
            if key in names:
                return None
            names.add(key)
            result.append(m)
        return result

    def install(self, env, d):
        got = member_packages(self.disk, d, self.docs.get(d)) if d not in self.broken else None
        for name, pkg in (got or {}).items():
            env.setdefault(name, [])
            if pkg not in env[name]:
                env[name].append(pkg)

    def source(self, env, declaring, src):
        if not isinstance(src, dict) or "path" not in src:
            return
        if any(k not in ("path", "editable") for k in src) or not isinstance(src["path"], str) or src.get("editable") is not True:
            return
        if src["path"].endswith((".whl", ".tar.gz", ".zip")):
            return
        m = _join(declaring, src["path"].rstrip("/"))
        if m is not None and self.has_pyproject(m):
            self.install(env, m)

    @staticmethod
    def find_source(sources, name):
        for k, v in (sources.items() if isinstance(sources, dict) else []):
            if _pep503(k) == name:
                return v
        return None

    @staticmethod
    def groups(doc):
        groups = doc.get("dependency-groups", {})
        if not isinstance(groups, dict):
            return []
        dflt = _uv(doc).get("default-groups", ["dev"])
        if dflt == "all":
            wanted = {_pep503(g) for g in groups}
        elif isinstance(dflt, list):
            wanted = {_pep503(g) for g in dflt if isinstance(g, str)}
        else:
            wanted = set()
        out = []
        for g in sorted(groups):
            if _pep503(g) in wanted:
                out.extend(_requirements(groups[g]))
        return out

    def workspace_env(self, root):
        members = self.members(root)
        if members is None:
            return {}
        env = {}
        for m in members:
            self.install(env, m)
        root_sources = _uv(self.docs[root]).get("sources", {})
        for m in members:
            own = _uv(self.docs[m]).get("sources", {})
            for name in _requirements(self.docs[m].get("project", {}).get("dependencies")):
                src = self.find_source(own, name)
                if src is not None:
                    self.source(env, m, src)
                else:
                    src = self.find_source(root_sources, name)
                    if src is not None:
                        self.source(env, root, src)
        for name in self.groups(self.docs[root]):
            src = self.find_source(root_sources, name)
            if src is not None:
                self.source(env, root, src)
        return env

    def standalone_env(self, d):
        doc = self.docs[d]
        sources = _uv(doc).get("sources", {})
        env = {}
        project = doc.get("project", {})
        for name in _requirements(project.get("dependencies") if isinstance(project, dict) else None) + self.groups(doc):
            src = self.find_source(sources, name)
            if src is not None:
                self.source(env, d, src)
        return env


def member_packages(disk, d, doc):
    """name -> package dir of an installed project, or None (not installed
    or refused)."""
    if doc is None:
        return None
    uv = _uv(doc)
    if "package" in uv:
        if uv["package"] is not True:
            return None
    elif "build-system" not in doc:
        return None
    if doc.get("build-system", {}).get("build-backend") != "hatchling.build":
        return None
    if disk.isfile(disk.join(d, "hatch.toml")):
        return None
    project = doc.get("project")
    if not isinstance(project, dict) or not isinstance(project.get("name"), str) or not project["name"]:
        return None
    if "name" in project.get("dynamic", []):
        return None
    # hatchling's is_valid_project_name, read over ASCII as Graphvein does
    # (its case-insensitive match also takes U+017F, U+0131, U+0130, U+212A
    # and a trailing newline; both refuse those).
    if not re.fullmatch(r"[A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9._-]*[A-Za-z0-9]", project["name"]):
        return None
    build = doc.get("tool", {}).get("hatch", {}).get("build", {})
    wheel = build.get("targets", {}).get("wheel", {})
    if any(k not in ("packages", "include", "targets") for k in build) or any(k not in ("packages", "include") for k in wheel):
        return None
    packages = wheel.get("packages", build.get("packages", []))
    include = wheel.get("include", build.get("include", []))
    if not packages:
        if include:
            return None
        raw = project["name"]
        norm = _pep503(raw)
        names = []
        for n in (re.sub(r"[^\w\d.]+", "_", raw, flags=re.UNICODE), re.sub(r"[^\w\d.]+", "_", norm, flags=re.UNICODE)):
            if n not in names:
                names.append(n)
        for n in names:
            for pkg in (disk.join(d, n), disk.join(disk.join(d, "src"), n)):
                if disk.isfile(pkg + "/__init__.py"):
                    return {n: pkg} if _is_identifier(n) else None
            if disk.isfile(disk.join(d, n + ".py")):
                return None
            if any(disk.isfile(x + "/" + n + "/__init__.py") for x in disk.dirs if (os.path.dirname(x) or ".") == d):
                return None
        return None
    packages = _strings(packages)
    if not packages:
        return None
    out, prefixes = {}, []
    for entry in packages:
        clean = entry[2:] if entry.startswith("./") else entry
        clean = clean.rstrip("/")
        pkg = _join(d, clean)
        if pkg is None or pkg == d:
            return None
        name = pkg.rsplit("/", 1)[-1]
        if not _is_identifier(name) or name in out or not disk.isfile(pkg + "/__init__.py"):
            return None
        out[name] = pkg
        prefixes.append(clean + "/")
    if include:
        include = _strings(include)
        if include is None:
            return None
        for pat in include:
            p = pat.lstrip("/")
            p = p[2:] if p.startswith("./") else p
            if not any(p.startswith(x) for x in prefixes):
                return None
    return out


def absolute(disk, roots, importer, dotted):
    """CPython's order for an absolute import, on top of the policy's search
    path: the interpreter's own modules (`__main__`, site hooks: refused),
    the meta path (frozen, start-up: python-stdlib; built in: python-stdlib,
    or refused when the repository would supply it, since another build may
    not build it in), the importer's script directory (`sys.path[0]` of
    `python dir/run.py` and pytest's `prepend` mode for a directory that is
    no package and no root: refused when it holds the name; for an implicit
    root: refused when it holds the name and answers differently from the
    policy), the policy's
    search path, and the standard library's own entries after it (a
    namespace-only repository name the standard library holds concretely:
    refused; one it holds on another platform only: either; a repository
    name that this interpreter's standard library has as an extension or
    another platform's module: refused, platform_module)."""
    parts = dotted.split(".")
    top = parts[0]
    if top in INTERPRETER:
        return {"spec": None, "refused": "interpreter_module"}
    if meta_served(top):
        return {"spec": None, "meta": True}
    d = os.path.dirname(importer) or "."
    script_dir = (not disk.isfile(disk.join(d, "__init__.py")) and d not in roots.explicit and d not in roots.implicit)
    if not builtin_served(top) and script_dir and path_finder(disk, top, [d]) is not None:
        return {"spec": None, "refused": "script_directory_module"}
    r = policy(disk, roots, importer, parts)
    implicit_dir = (not disk.isfile(disk.join(d, "__init__.py")) and d not in roots.explicit and d in roots.implicit)
    if not builtin_served(top) and implicit_dir and r.get("spec") is not None and path_finder(disk, top, [d]) is not None:
        # An implicit root is searched after the explicit ancestors by the
        # policy, but `python dir/run.py` puts the directory first: refused
        # only when CPython's PathFinder over that directory alone gives
        # another answer than the policy's.
        own, _ = find_dotted(disk, parts, [d])
        if own is None or own.origin != r["spec"].origin or list(own.locations or []) != list(r["spec"].locations or []):
            return {"spec": None, "refused": "script_directory_module"}
    in_repo = r.get("spec") is not None or r.get("top_in_repo") or r.get("ambiguous")
    if builtin_served(top):
        # Built in here; the contract may refuse it when the repository
        # would supply it (another build may not build it in).
        return {"spec": None, "meta": True, "either": bool(in_repo)}
    if r.get("spec") is not None and r.get("top_namespace") and stdlib_name(top):
        spec = stdlib_spec(top)
        if spec is not None and spec.origin is not None:
            return {"spec": None, "refused": "namespace_hides_concrete"}
        r["either"] = True
    elif in_repo and top in sys.stdlib_module_names and not stdlib_pure(top):
        return {"spec": None, "refused": "platform_module"}
    return r


def policy(disk, roots, importer, parts):
    """Policy: ancestor explicit roots deepest first as one search path; then
    each other explicit root alone (exactly one hit resolves, several are
    ambiguous); then implicit ancestor roots deepest first. Within each step
    the import system's own rules apply. `top_namespace`: the first
    component of the step that answered is namespace portions only."""

    def ns(path):
        t, _ = find_dotted(disk, parts[:1], path)
        return t is not None and t.origin is None

    anc = sorted([r for r in roots.explicit if Roots.ancestor(r, importer)], key=lambda r: (-len(r), r))
    site = roots.site_for(importer)
    if site:
        # Row PS-M2: the declared environment's site-packages comes after
        # the ancestor roots; PathFinder decides (a namespace portion found
        # earlier does not stop the search, a concrete package later wins).
        anc = anc + [SiteEntry(site)]
    try:
        spec, err = find_dotted(disk, parts, anc)
    except Ambiguous as a:
        return {"spec": None, "ambiguous": a.inits, "step": "site"}
    if spec is not None:
        return {"spec": spec, "step": "ancestor", "top_namespace": ns(anc)}
    if err == "parent_not_package" or err == "submodule_not_found":
        top, _ = find_dotted(disk, parts[:1], anc)
        if top is not None and top.concrete():
            # Python stops here: a concrete top-level module or regular
            # package binds the name for the whole search path, and no later
            # entry can supply the rest. (A namespace top does not: a
            # concrete package on any later entry wins over it, so the
            # policy's later steps are followed.)
            return {"spec": None, "error": err, "step": "ancestor", "top": top, "top_in_repo": True}
    hits = []
    top_in_repo = False
    for r in roots.explicit:
        if Roots.ancestor(r, importer):
            continue
        s, _ = find_dotted(disk, parts, [r])
        t, _ = find_dotted(disk, parts[:1], [r])
        top_in_repo = top_in_repo or (t is not None and t.concrete())
        if s is not None:
            hits.append((s, r))
    if len(hits) == 1:
        return {"spec": hits[0][0], "step": "sibling", "top_namespace": ns([hits[0][1]])}
    if len(hits) > 1:
        return {"spec": None, "ambiguous": sorted((h.origin or ",".join(h.locations)) for h, _ in hits), "step": "sibling"}
    imp = [r for r in roots.implicit if Roots.ancestor(r, importer)]
    spec, err2 = find_dotted(disk, parts, imp)
    if spec is not None:
        return {"spec": spec, "step": "implicit", "top_namespace": ns(imp)}
    t, _ = find_dotted(disk, parts[:1], imp)
    top_in_repo = top_in_repo or (t is not None and t.concrete())
    return {"spec": None, "error": err or "not_found", "step": "none", "top_in_repo": top_in_repo}


def package_parts_under(root, importer):
    d = os.path.dirname(importer) or "."
    if root == ".":
        rel = d
    else:
        rel = d[len(root) + 1:] if d != root else "."
    return [] if rel == "." else rel.split("/")


def relative(disk, roots, py_set, importer, level, module):
    """Relative import: __package__ is the importer's directory chain under a
    root; `level` dots keep len(__package__) - (level-1) components and need
    at least `level` components (CPython _resolve_name). Validity is
    reported per ancestor root (explicit and implicit). The target is the
    directory-relative module found in that package's directory."""
    d = os.path.dirname(importer) or "."
    valid = []
    invalid = []
    anc = [r for r in roots.explicit + roots.implicit if Roots.ancestor(r, importer)]
    for r in sorted(set(anc)):
        parts = package_parts_under(r, importer)
        if len(parts) >= level:
            valid.append(r)
        else:
            invalid.append(r)
    # the repository root as a regular package extends the chain outward
    root_is_package = "__init__.py" in py_set
    base = d
    for _ in range(level - 1):
        if base == ".":
            base = None
            break
        base = os.path.dirname(base) or "."
    out = {"valid_roots": valid, "invalid_roots": invalid, "root_is_package": root_is_package}
    if base is None:
        out["spec"] = None
        out["error"] = "above_repository"
        return out
    if module:
        spec, err = find_dotted(disk, module.split("."), [base])
        out["spec"] = spec
        if spec is None:
            out["error"] = err
    else:
        # `from . import x`: the package itself
        init = None
        for suf in SUFFIXES:
            c = disk.join(base, "__init__" + suf)
            if disk.isfile(c):
                init = c
                break
        out["spec"] = Spec("", init, [base])
    return out


# Module-level bindings of a module (for `from M import n`)


def module_bindings(tree, pkg_name_parts):
    """name -> list of (site kind, node, conditional) for module scope.
    Also the set of submodule names the module itself imports (each sets
    the attribute on the package), and whether a star import occurs."""
    sites = collections.defaultdict(list)
    sub_imports = set()
    star = []

    def bind(name, kind, node, cond):
        sites[name].append((kind, node, cond))

    def targets(t, kind, node, cond):
        for e in ast.walk(t):
            if isinstance(e, ast.Name) and isinstance(e.ctx, (ast.Store, ast.Del)):
                bind(e.id, kind, node, cond)

    def visit(stmts, cond):
        for s in stmts:
            if isinstance(s, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                bind(s.name, "def", s, cond)
                for n in ast.walk(s):
                    if isinstance(n, ast.Global):
                        for g in n.names:
                            bind(g, "global", n, True)
            elif isinstance(s, ast.Import):
                for a in s.names:
                    bind(a.asname or a.name.split(".")[0], "import", (s, a), cond)
            elif isinstance(s, ast.ImportFrom):
                if s.level == 1 and s.module:
                    sub_imports.add(s.module.split(".")[0])
                if s.level == 1 and not s.module:
                    for a in s.names:
                        sub_imports.add(a.name)
                if s.level == 0 and s.module and pkg_name_parts and s.module.split(".")[:len(pkg_name_parts)] == pkg_name_parts and len(s.module.split(".")) > len(pkg_name_parts):
                    sub_imports.add(s.module.split(".")[len(pkg_name_parts)])
                for a in s.names:
                    if a.name == "*":
                        star.append(s)
                    else:
                        bind(a.asname or a.name, "importfrom", (s, a), cond)
            elif isinstance(s, (ast.Assign, ast.AnnAssign, ast.AugAssign)):
                ts = s.targets if isinstance(s, ast.Assign) else [s.target]
                if isinstance(s, ast.AnnAssign) and s.value is None:
                    continue
                for t in ts:
                    targets(t, "assign", s, cond)
            elif isinstance(s, ast.Delete):
                for t in s.targets:
                    targets(t, "del", s, True)
            elif isinstance(s, ast.TypeAlias):
                targets(s.name, "assign", s, cond)
            elif isinstance(s, (ast.For, ast.AsyncFor)):
                targets(s.target, "for", s, True)
                visit(s.body, True)
                visit(s.orelse, True)
            elif isinstance(s, ast.While):
                visit(s.body, True)
                visit(s.orelse, True)
            elif isinstance(s, ast.If):
                visit(s.body, True)
                visit(s.orelse, True)
            elif isinstance(s, (ast.With, ast.AsyncWith)):
                for it in s.items:
                    if it.optional_vars is not None:
                        targets(it.optional_vars, "with", s, cond)
                visit(s.body, cond)
            elif isinstance(s, (ast.Try, ast.TryStar)):
                visit(s.body, True)
                for h in s.handlers:
                    if h.name:
                        bind(h.name, "except", h, True)
                    visit(h.body, True)
                visit(s.orelse, True)
                visit(s.finalbody, cond)
            elif isinstance(s, ast.Match):
                for c in s.cases:
                    for n in ast.walk(c.pattern):
                        nm = getattr(n, "name", None)
                        if isinstance(nm, str):
                            bind(nm, "match", c, True)
                    visit(c.body, True)
            # walrus at module level
            for n in ast.walk(s) if not isinstance(s, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef, ast.Lambda)) else []:
                if isinstance(n, ast.NamedExpr) and isinstance(n.target, ast.Name):
                    bind(n.target.id, "walrus", n, True)
                    break

    visit(tree.body, False)
    return sites, sub_imports, star


# --------------------------------------------------------------------------
# Main comparison


class Oracle:
    def __init__(self, snap, root, corpus):
        self.snap = snap
        self.root = root
        self.corpus = corpus
        self.out = []
        self.counts = collections.Counter()
        self.disk = Disk(root)
        self.py_files = snap.python_files()
        self.py_set = set(self.py_files)
        self.roots = Roots(snap, self.py_files)
        self.roots.env = Environments(self.disk)
        self.trees = {}
        self.sources = {}
        self.census = collections.Counter()
        self.refused_accepted = set()

    def rec(self, kind, cls, file, **kw):
        r = {"kind": kind, "corpus": self.corpus, "class": cls, "file": file, "gate": is_gate(cls)}
        r.update(kw)
        self.out.append(r)
        self.counts[(kind, cls)] += 1

    def load(self, f):
        if f in self.trees:
            return self.trees[f]
        raw = open(os.path.join(self.root, f), "rb").read()
        tree = None
        err = None
        try:
            src = Source(raw)
            self.sources[f] = src
            tree = ast.parse(raw, filename=f)
            src.decoded_text = raw.decode(src.encoding if src.encoding != "utf-8-sig" else "utf-8-sig").replace("\r\n", "\n").replace("\r", "\n")
        except (SyntaxError, ValueError, UnicodeDecodeError, LookupError) as e:
            err = "%s: %s" % (type(e).__name__, e)
            tree = None
        self.trees[f] = (tree, err)
        return self.trees[f]

    # ---- files

    def check_files(self):
        disk_py = sorted(p for p in self.disk.files if p.endswith((".py", ".pyi")))
        snap_py = set(self.py_files)
        for p in disk_py:
            self.census["disk_py_files"] += 1
            if p.endswith(".pyi"):
                self.census["pyi_files"] += 1
            if p not in snap_py:
                raw = open(os.path.join(self.root, p), "rb").read()
                reason = "not_scanned"
                try:
                    raw.decode("utf-8")
                except UnicodeDecodeError:
                    reason = "not_utf8"
                if b"\x00" in raw:
                    reason = "nul_byte"
                self.rec("file", "F-missing-" + reason, p)
        for p in self.py_files:
            self.census["snapshot_python_files"] += 1
            raw = open(os.path.join(self.root, p), "rb").read()
            rec = self.snap.files[p]
            if rec["content_hash"] != sha(raw):
                self.rec("file", "F-hash", p)
            if rec["size"] != len(raw):
                self.rec("file", "F-size", p)
            if not p.endswith((".py", ".pyi")):
                self.census["python_by_shebang"] += 1
            if b"\r" in raw:
                lone = raw.count(b"\r") != raw.count(b"\r\n")
                self.census["files_lone_cr" if lone else "files_crlf"] += 1
            if raw.startswith(b"\xef\xbb\xbf"):
                self.census["files_bom"] += 1
            if b"\x0c" in raw:
                self.census["files_form_feed"] += 1
            if b"\t" in raw:
                self.census["files_tab"] += 1

    # ---- declarations

    def snapshot_decls(self):
        by_key = {}
        for ident, n in self.snap.nodes.items():
            if n["kind"] in DECL_KINDS and n.get("file") in self.py_set:
                by_key[(n["kind"], n["file"], n["qualified_name"])] = n
        return by_key

    def check_decls(self):
        snap = self.snapshot_decls()
        contains = collections.defaultdict(list)
        for e in self.snap.edges:
            if e["kind"] == "contains":
                contains[e["target"]].append(e)
        seen = set()
        for f in self.py_files:
            if any(d["code"] == "file_too_large" for d in self.snap.diagnostics.get(f, [])):
                # Over the parse limit: the indexer reads no declaration or
                # import of it (an omission, not a disagreement).
                self.rec("decl", "N-file-too-large", f)
                self.refused_accepted.add(f)
                continue
            tree, err = self.load(f)
            if tree is None:
                self.census["ast_rejected_files"] += 1
                n = [k for k in snap if k[1] == f]
                diag = [d["code"] for d in self.snap.diagnostics.get(f, [])]
                # Row PS-F2: a rejected file claims nothing beyond its file.
                self.rec("decl", "D-claimed-in-rejected-file" if n else "N-rejected-file", f, detail=err, graphvein_decls=len(n), syntax_error_diag="syntax_error" in diag)
                for k in n:
                    seen.add(k)
                continue
            try:
                compile(tree, f, "exec", dont_inherit=True)
            except SyntaxError as e:
                self.census["compile_rejected_files"] += 1
                self.rec("decl", "N-compile-rejected-file", f, detail=str(e))
            if any(d["code"] in ("syntax_error", "python_syntax_rejected") for d in self.snap.diagnostics.get(f, [])):
                # Graphvein refused a file Python accepts: an omission of
                # every claim in it (reported, not a wrong fact).
                self.rec("decl", "O-refused-but-ast-accepts", f)
                self.refused_accepted.add(f)
                continue
            o = DeclOracle(f, self.sources[f], tree)
            try:
                decls = o.run()
            except (UnicodeDecodeError, IndexError) as e:
                self.rec("decl", "D-oracle-error", f, detail=repr(e))
                continue
            for n in tree.body:
                if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "__all__" for t in n.targets):
                    self.census["files_with___all__"] += 1
            for n in ast.walk(tree):
                if isinstance(n, ast.ImportFrom) and any(a.name == "*" for a in n.names):
                    self.census["star_imports"] += 1
            for k, v in o.forms.items():
                self.census["form_" + k] += v
            for d in decls:
                key = (d["kind"], f, d["q"])
                self.census["oracle_" + d["kind"]] += 1
                n = snap.get(key)
                if n is None:
                    self.rec("decl", "D-missing", f, q=d["q"], decl_kind=d["kind"], span=d["span"])
                    continue
                seen.add(key)
                bad = []
                sp = n.get("span") or {}
                gs = [sp.get("start_line"), sp.get("start_column"), sp.get("end_line"), sp.get("end_column")]
                if gs != d["span"]:
                    bad.append("span")
                if n.get("content_hash") != d["hash"]:
                    bad.append("hash")
                if n.get("name") != d["name"]:
                    bad.append("name")
                gdec = (n.get("attributes") or {}).get("decorators")
                if (gdec or None) != (d["decorators"] or None):
                    bad.append("decorators")
                # contains edge from the owner
                ce = contains.get(n["identity"], [])
                owner_ok = False
                if len(ce) == 1:
                    src = ce[0]["source"]
                    if d["owner"] is None:
                        owner_ok = src.startswith("file:") and self.snap.nodes.get(src, {}).get("file") == f
                    else:
                        on = self.snap.nodes.get(src)
                        owner_ok = on is not None and on["kind"] == d["owner"][0] and on["qualified_name"] == d["owner"][1] and on["file"] == f
                    ev = ce[0].get("evidence") or []
                    if [ {"start_line": gs[0], "start_column": gs[1], "end_line": gs[2], "end_column": gs[3]} ] != [x.get("span") for x in ev]:
                        bad.append("contains_evidence")
                if not owner_ok:
                    bad.append("contains")
                if bad:
                    cls = "D-differs"
                    if "span" in bad:
                        cls = "D-span-" + self.span_class(f, gs, d["span"])
                    self.rec("decl", cls, f, q=d["q"], decl_kind=d["kind"], fields=bad, graphvein_span=gs, oracle_span=d["span"],
                             graphvein_decorators=gdec, oracle_decorators=d["decorators"])
                else:
                    self.census["agree_" + d["kind"]] += 1
        for key, n in snap.items():
            if key not in seen:
                self.rec("decl", "D-extra", key[1], q=key[2], decl_kind=key[0], span=[n["span"]["start_line"], n["span"]["start_column"], n["span"]["end_line"], n["span"]["end_column"]])
            self.census["graphvein_" + key[0]] += 1

    def span_class(self, f, gs, os_):
        """Name what Graphvein's span adds to or drops from the ast span."""
        src = self.sources[f]
        if gs[:2] != os_[:2]:
            return "start"
        if (gs[2], gs[3]) < (os_[2], os_[3]):
            return "end_short"
        a = src.offset(os_[2], os_[3])
        b = src.offset(gs[2], gs[3])
        extra = src.raw[a:b].replace(b"\r\n", b"\n").replace(b"\r", b"\n")
        lines = extra.split(b"\n")
        kinds = set()
        for i, ln in enumerate(lines):
            st = ln.strip(b" \t\x0c")
            if not st:
                continue
            if not st.startswith(b"#"):
                return "end_long_code"
            indent = len(ln) - len(ln.lstrip(b" \t\x0c"))
            if i > 0 and indent <= os_[1]:
                kinds.add("dedented")
            else:
                kinds.add("indented")
        if "dedented" in kinds:
            return "trailing_comment_dedented"
        if len(lines) == 1:
            return "trailing_comment_same_line"
        return "trailing_comment"

    # ---- imports

    def graphvein_imports(self):
        """(importer, span tuple) -> set of targets ("file:<path>" or
        "external:<name>")."""
        out = collections.defaultdict(set)
        for e in self.snap.edges:
            if e["kind"] != "imports":
                continue
            s = self.snap.nodes.get(e["source"])
            if s is None or s["kind"] != "file" or s["file"] not in self.py_set:
                continue
            t = self.snap.nodes.get(e["target"])
            if t is None:
                tgt = "?" + e["target"]
            elif t["kind"] == "file":
                tgt = "file:" + t["file"]
            elif t["kind"] == "external":
                tgt = "external:" + (t.get("attributes") or {}).get("ecosystem", "") + ":" + t["name"]
            else:
                tgt = t["kind"] + ":" + t["identity"]
            for ev in e.get("evidence") or []:
                sp = ev["span"]
                out[(s["file"], (sp["start_line"], sp["start_column"], sp["end_line"], sp["end_column"]))].add(tgt)
        return out

    def bound_module(self, mod_spec, name, depth=0):
        """For `from M import name`: what module (spec) does `name` denote,
        per the import system's from-list rule and M's own module-level
        bindings. Returns (spec or None, status)."""
        if depth > 8:
            return None, "undecided_depth"
        if mod_spec is None:
            return None, "no_module"
        sub = None
        if mod_spec.locations is not None:
            sub = path_finder(self.disk, name, mod_spec.locations)
        if mod_spec.origin is None or not mod_spec.origin.endswith(".py"):
            # namespace package (or non-source module): only a submodule can
            # be found without running code
            if sub is not None:
                return sub, "submodule"
            return None, "attribute_unknown" if mod_spec.origin else "not_found"
        tree, err = self.load(mod_spec.origin) if mod_spec.origin in self.py_set or os.path.isfile(os.path.join(self.root, mod_spec.origin)) else (None, "missing")
        if tree is None:
            # Python's import of the package fails: undecided either way.
            return None, "undecided_init_unparsed" if sub is not None else "init_unparsed"
        pkg_parts = None
        sites, sub_imports, stars = module_bindings(tree, pkg_parts)
        own = [s for s in sites.get(name, [])]
        if not own and "__getattr__" in sites:
            # PEP 562: _handle_fromlist asks hasattr(module, name), which
            # calls the module's __getattr__ before any submodule import.
            return None, "undecided_module_getattr"
        star = self.star_may_bind(mod_spec.origin, stars, name, depth)
        if sub is not None:
            if not own and not star:
                # The contract refuses a submodule of a package whose
                # __init__ has any star import (Graphvein does not read the
                # star's source): an explained omission.
                if stars:
                    return sub, "submodule_star_rule"
                return sub, "submodule"
            if not own and star:
                return None, "undecided_star_and_submodule"
            # __init__ binds the name: attribute wins at first import, but a
            # later import of the submodule (anywhere) rebinds it
            only_self = all(k in ("importfrom",) and isinstance(n, tuple) and n[0].level == 1 and not n[0].module and n[1].name == name for k, n, c in own)
            if only_self:
                return sub, "submodule_imported_by_init"
            if not self.binds_from_submodule(mod_spec, own, sub):
                return None, "undecided_binding_and_submodule"
            # The binding imports from the submodule itself: the import
            # loads it (setting the attribute) before the binding, and a
            # later import of a loaded module sets no attribute again, so
            # the binding is final.
        if not own:
            return None, "undecided_star" if star else "no_binding"
        if len(own) > 1 or own[0][2]:
            return None, "undecided_conditional_or_multiple"
        kind, node, _ = own[0]
        if kind == "import":
            s, a = node
            target = a.name if a.asname else a.name.split(".")[0]
            r = absolute(self.disk, self.roots, mod_spec.origin, target)
            return (r.get("spec"), "reexport_import") if r.get("spec") is not None else (None, "reexport_external")
        if kind == "importfrom":
            s, a = node
            if s.level:
                r = relative(self.disk, self.roots, self.py_set, mod_spec.origin, s.level, s.module)
            else:
                r = absolute(self.disk, self.roots, mod_spec.origin, s.module)
            inner = r.get("spec")
            if inner is None:
                return None, "reexport_external"
            b, st = self.bound_module(inner, a.name, depth + 1)
            return b, "reexport:" + st
        return None, "value_binding"

    def binds_from_submodule(self, mod_spec, own, sub):
        """One unconditional `from X import ...` binding whose module X is
        the same-named submodule `sub` (relative or absolute)."""
        if len(own) != 1 or own[0][2] or own[0][0] != "importfrom" or sub.origin is None:
            return False
        s, a = own[0][1]
        if not s.module:
            return False
        if s.level:
            r = relative(self.disk, self.roots, self.py_set, mod_spec.origin, s.level, s.module)
        else:
            r = absolute(self.disk, self.roots, mod_spec.origin, s.module)
        spec = r.get("spec")
        return spec is not None and spec.origin == sub.origin

    def star_may_bind(self, origin, stars, name, depth):
        """Whether one of the module's `from X import *` statements may bind
        `name`: X's `__all__` lists it, or X binds it at module level (and
        it is public), or X cannot be read."""
        for s in stars:
            if s.level:
                r = relative(self.disk, self.roots, self.py_set, origin, s.level, s.module)
            else:
                r = absolute(self.disk, self.roots, origin, s.module)
            spec = r.get("spec")
            if spec is None or spec.origin is None or not spec.origin.endswith(".py"):
                return True
            tree, err = self.load(spec.origin)
            if tree is None or depth > 8:
                return True
            sites, _, inner = module_bindings(tree, None)
            if "__all__" in sites:
                allv = None
                for k, n, c in sites["__all__"]:
                    if k == "assign" and isinstance(n, ast.Assign) and isinstance(n.value, (ast.List, ast.Tuple)) and len(sites["__all__"]) == 1:
                        allv = [e.value for e in n.value.elts if isinstance(e, ast.Constant)]
                if allv is None or name in allv:
                    return True
                continue
            if name.startswith("_"):
                continue
            if name in sites:
                return True
            if inner and self.star_may_bind(spec.origin, inner, name, depth + 1):
                return True
        return False

    def ecosystem(self, top):
        """Row PS-I5: a standard-library name of this interpreter (listed
        in sys.stdlib_module_names, served by its meta path, or a module its
        standard-library directory holds, such as CPython's `test` package)
        is a python-stdlib external; any other name is a pypi external
        (PS-I4)."""
        if top in sys.stdlib_module_names or builtin_served(top) or meta_served(top) or stdlib_name(top):
            return "python-stdlib"
        return "pypi"

    def init_has_star(self, spec):
        if spec.origin is None or not spec.origin.endswith(".py"):
            return False
        tree, _ = self.load(spec.origin)
        if tree is None:
            return False
        return bool(module_bindings(tree, None)[2])

    def check_imports(self):
        g = self.graphvein_imports()
        matched = set()
        by_file = collections.defaultdict(list)
        for key in g:
            by_file[key[0]].append(key)
        for f in self.py_files:
            tree, err = self.load(f)
            if tree is None or f in self.refused_accepted:
                for key in by_file.get(f, []):
                    matched.add(key)
                    if tree is None:
                        # Row PS-F2: a rejected file claims no import edge.
                        self.rec("import", "I-claimed-in-rejected-file", f, span=list(key[1]), targets=sorted(g[key]))
                continue
            src = self.sources[f]
            context = {}

            def mark(stmts, ctx):
                for st in stmts:
                    if isinstance(st, (ast.Import, ast.ImportFrom)):
                        context[id(st)] = ctx
                    sub = ctx
                    if isinstance(st, (ast.FunctionDef, ast.AsyncFunctionDef)):
                        sub = ctx | {"function"}
                    elif isinstance(st, ast.ClassDef):
                        sub = ctx | {"class"}
                    elif isinstance(st, ast.If):
                        sub = ctx | ({"type_checking"} if is_type_checking(st.test) else {"if"})
                    elif isinstance(st, (ast.Try, ast.TryStar)):
                        sub = ctx | {"try"}
                    elif isinstance(st, (ast.For, ast.AsyncFor, ast.While, ast.With, ast.AsyncWith, ast.Match)):
                        sub = ctx | {"block"}
                    for field in ("body", "orelse", "finalbody"):
                        mark(getattr(st, field, []) or [], sub)
                    for h in getattr(st, "handlers", []) or []:
                        mark(h.body, sub)
                    for c in getattr(st, "cases", []) or []:
                        mark(c.body, sub)

            mark(tree.body, frozenset())
            for node in ast.walk(tree):
                if not isinstance(node, (ast.Import, ast.ImportFrom)):
                    continue
                for c in context.get(id(node), ()):
                    self.census["import_in_" + c] += 1
                if not context.get(id(node)):
                    self.census["import_in_module_top"] += 1
                sl, sc = src.pos(node.lineno, node.col_offset)
                el, ec = src.pos(node.end_lineno, node.end_col_offset)
                span = (sl, sc, el, ec)
                key = (f, span)
                gt = g.get(key, set())
                if key in g:
                    matched.add(key)
                expect, omitted, undecided, notes, optional = self.expected(f, node)
                stmt = ast.get_source_segment(src.decoded_text, node)[:200]
                self.census["import_statements"] += 1
                self.census["import_" + ("from" if isinstance(node, ast.ImportFrom) else "plain")] += 1
                for n in notes:
                    self.census["import_form_" + n] += 1
                gfiles = {t for t in gt if t.startswith("file:")}
                gext = {t for t in gt if not t.startswith("file:")}
                efiles = {t for t in expect if t.startswith("file:")}
                eext = {t for t in expect if not t.startswith("file:")}
                for n in sorted(set(notes) & NOTABLE):
                    self.rec("site", "N-" + n, f, span=list(span), stmt=stmt, graphvein=sorted(gt), oracle=sorted(expect))
                for t in sorted(gfiles - efiles - set(optional)):
                    if t in undecided:
                        cls = "I-claimed-undecided"
                    elif omitted.get(t) == "relative_beyond_top" or omitted.get("*") == "relative_beyond_top":
                        cls = "I-relative-beyond-top"
                    else:
                        cls = "I-wrong-edge"
                    self.rec("import", cls, f, span=list(span), target=t, why=omitted.get(t) or undecided.get(t), stmt=stmt, notes=notes)
                for t in sorted(gext - eext - set(optional)):
                    self.rec("import", "I-external-differs", f, span=list(span), graphvein=t, oracle=sorted(eext), why=omitted.get(t), stmt=stmt, notes=notes)
                for t in sorted(efiles - gfiles):
                    self.rec("import", "I-missing-edge", f, span=list(span), target=t, stmt=stmt, notes=notes)
                for t in sorted(eext - gext):
                    self.rec("import", "I-external-missing", f, span=list(span), oracle=t, graphvein=sorted(gt), stmt=stmt, notes=notes)
                for t, why in sorted(omitted.items()):
                    if t not in gt:
                        self.rec("import", "O-" + why, f, span=list(span), target=t, stmt=stmt)
                for t, why in sorted(undecided.items()):
                    if t.startswith("?"):
                        self.rec("import", "O-" + why, f, span=list(span), target=t, stmt=stmt)
                self.census["edges_graphvein_file"] += len(gfiles)
                self.census["edges_graphvein_external"] += len(gext)
                self.census["edges_agree_file"] += len(gfiles & efiles)
                self.census["edges_agree_external"] += len(gext & eext)
        for key, ts in g.items():
            if key in matched:
                continue
            self.rec("import", "I-span-unmatched", key[0], span=list(key[1]), targets=sorted(ts))

    def expected(self, f, node):
        """What the contract claims for one statement, from Python's own
        rules: `expect` (targets "file:<path>" or "external:<eco>:<name>"),
        `omitted` (target -> the contract's reason for claiming nothing
        though Python names it), `undecided` (target -> class: Python's
        answer depends on run time, so no claim), `optional` (targets the
        contract may claim or refuse: CPython's answer depends on the build or
        platform) and notes."""
        expect, omitted, undecided, notes = set(), {}, {}, []
        optional = {}
        either = [False]

        def claim(t):
            if either[0]:
                optional[t] = "platform_module"
            else:
                expect.add(t)

        def add_file(origin):
            t = "file:" + origin
            if origin == f:
                omitted[t] = "self_import"
            elif origin not in self.py_set or not origin.endswith(".py"):
                notes.append("target_not_scanned")
                omitted[t] = "shadowed_module"
            else:
                claim(t)

        def add_external(module, r):
            top = module.split(".")[0]
            t = "external:" + self.ecosystem(top) + ":" + top
            if r.get("top_in_repo"):
                notes.append("external_but_top_in_repo")
                omitted[t] = "unresolved_in_repository"
            else:
                claim(t)

        def special(module, r):
            """The meta-path, interpreter, script-directory and standard
            library outcomes of absolute(); True when r is settled."""
            top = module.split(".")[0]
            either[0] = bool(r.get("either"))
            if r.get("refused"):
                notes.append(r["refused"])
                omitted["?" + module] = r["refused"]
                return True
            if r.get("meta"):
                notes.append("meta_path")
                claim("external:python-stdlib:" + top)
                return True
            return False

        if isinstance(node, ast.Import):
            for a in node.names:
                if a.asname:
                    notes.append("aliased")
                if "." in a.name:
                    notes.append("dotted")
                r = absolute(self.disk, self.roots, f, a.name)
                if special(a.name, r):
                    continue
                if r.get("ambiguous"):
                    notes.append("ambiguous")
                    continue
                spec = r.get("spec")
                if spec is None:
                    add_external(a.name, r)
                elif spec.origin is None:
                    notes.append("namespace_module")
                else:
                    add_file(spec.origin)
            return expect, omitted, undecided, notes, optional
        level, module = node.level, node.module
        if module == "__future__" and not level:
            notes.append("future")
            omitted["external:python-stdlib:__future__"] = "future_import"
            return expect, omitted, undecided, notes, optional
        if level:
            notes.append("relative")
            r = relative(self.disk, self.roots, self.py_set, f, level, module)
            if r.get("invalid_roots"):
                notes.append("relative_beyond_top_under_some_root")
            if not r.get("valid_roots") and not r.get("root_is_package"):
                # Row PS-I3: Python raises ImportError under every root.
                notes.append("relative_beyond_top_under_every_root")
                omitted["*"] = "relative_beyond_top"
                return expect, omitted, undecided, notes, optional
        else:
            r = absolute(self.disk, self.roots, f, module)
            if special(module, r):
                return expect, omitted, undecided, notes, optional
        if r.get("ambiguous"):
            notes.append("ambiguous")
            return expect, omitted, undecided, notes, optional
        spec = r.get("spec")
        if spec is None:
            if not level:
                add_external(module, r)
            else:
                notes.append("relative_unresolved")
            return expect, omitted, undecided, notes, optional
        if spec.origin is not None:
            if spec.origin not in self.py_set or not spec.origin.endswith(".py"):
                # Row PS-I1: the module is a file Graphvein does not read;
                # the whole statement claims nothing.
                notes.append("target_not_scanned")
                omitted["file:" + spec.origin] = "shadowed_module"
                return expect, omitted, undecided, notes, optional
            add_file(spec.origin)
        else:
            notes.append("namespace_module")
        for a in node.names:
            if a.name == "*":
                notes.append("star")
                continue
            b, status = self.bound_module(spec, a.name)
            last = status.split(":")[-1]
            notes.append("name:" + last)
            if b is not None and b.origin is not None:
                if last == "submodule_imported_by_init":
                    omitted["file:" + b.origin] = "submodule_after_self_binding"
                elif last == "submodule_star_rule":
                    omitted["file:" + b.origin] = "undecided_name_star_rule"
                else:
                    add_file(b.origin)
            elif "undecided" in status or status in ("init_unparsed", "attribute_unknown"):
                undecided["?" + a.name] = "undecided_name"
                for cand in self.candidates(spec, a.name):
                    undecided[cand] = "undecided_name"
        return expect, omitted, undecided, notes, optional

    def candidates(self, spec, name):
        out = []
        if spec.locations:
            s = path_finder(self.disk, name, spec.locations)
            if s is not None and s.origin:
                out.append("file:" + s.origin)
        return out

    def run(self):
        self.check_files()
        self.check_decls()
        self.check_imports()
        summary = {"corpus": self.corpus, "roots_explicit": self.roots.explicit, "roots_implicit_count": len(self.roots.implicit),
                   "census": dict(sorted(self.census.items())),
                   "disagreements": {"%s/%s" % k: v for k, v in sorted(self.counts.items())},
                   "gate": sum(v for k, v in self.counts.items() if is_gate(k[1])),
                   "python": "%d.%d.%d" % sys.version_info[:3]}
        return summary


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--snapshot", required=True)
    ap.add_argument("--root", required=True)
    ap.add_argument("--corpus", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--summary")
    a = ap.parse_args()
    if sys.version_info[:2] != REFERENCE:
        sys.exit("oracle.py needs Python %d.%d (the contract's reference), not %s" % (REFERENCE + (sys.version.split()[0],)))
    snap = Snapshot(a.snapshot)
    o = Oracle(snap, a.root, a.corpus)
    summary = o.run()
    with open(a.out, "w") as f:
        for r in o.out:
            f.write(json.dumps(r, sort_keys=True) + "\n")
    s = json.dumps(summary, indent=1, sort_keys=True)
    if a.summary:
        with open(a.summary, "w") as f:
            f.write(s + "\n")
    print(s)


if __name__ == "__main__":
    sys.setrecursionlimit(10000)
    main()
