//! Declaration oracle for Graphvein's Rust structure contract (T020 S4).
//!
//! Reads each Rust file named on stdin (one path per line, relative to the
//! root given as the only argument) as raw bytes, parses it with `syn`
//! (independent of tree-sitter), and prints one JSON line per file with every
//! item site: its position class, the kind and qualified name the identity
//! rules of `docs/lang/identity.md` ("Rust") give it, and its byte span (outer
//! attributes and doc comments excluded, as written in the source).
//!
//! It never expands macros, evaluates `cfg`, or runs corpus code. The module
//! tree and `use` resolution are checked separately by `compare.go` with
//! rustc's own resolver (`-Zunpretty=hir-tree`).
//!
//! Build (scratch target dir, pinned lock file):
//!   CARGO_TARGET_DIR=<scratch>/target cargo build --release --locked
use proc_macro2::{Span, TokenStream};
use quote::ToTokens;
use std::fmt::Write as _;
use std::io::{self, BufRead, Write};
use syn::spanned::Spanned;

struct Site {
    pos: &'static str,
    kind: &'static str,
    name: String,
    q: String,
    owner: String,
    /// The site that contains this one (a trait for its methods, the
    /// enclosing function for an item in a function body), if any.
    parent: Option<usize>,
    start: usize,
    end: usize,
    cfg: bool,
}

/// Where an item sits. `Module`: a module body (file root or inline
/// module). `Fn`: a claimed function body (or, for its direct items, a
/// trait inside one): items are named under `base` and contained by the site
/// `owner` (row RS-D10). `Unclaimed`: any other block (a const or static
/// initializer, a block `mod`, the body of an unclaimed item): its items are
/// "local" sites that claim nothing.
#[derive(Clone)]
enum Scope {
    Module,
    Fn { owner: usize, base: String },
    Unclaimed(String),
}

impl Scope {
    /// Position class of a plain item, its owner text and its parent site.
    fn place(&self, module_pos: &'static str) -> (&'static str, String, Option<usize>) {
        match self {
            Scope::Module => (module_pos, "file".to_string(), None),
            Scope::Fn { owner, .. } => ("fn_item", "fn".to_string(), Some(*owner)),
            Scope::Unclaimed(o) => ("local", o.clone(), None),
        }
    }

    /// Qualified name prefix of the items in this scope.
    fn prefix<'s>(&'s self, module: &'s str) -> &'s str {
        match self {
            Scope::Fn { base, .. } => base,
            _ => module,
        }
    }
}

struct Ctx<'a> {
    src: &'a str,
    base: usize,
    sites: Vec<Site>,
    census: std::collections::BTreeMap<String, usize>,
}

fn json_str(s: &str) -> String {
    let mut out = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => {
                let _ = write!(out, "\\u{:04x}", c as u32);
            }
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

fn join(a: &str, b: &str) -> String {
    if a.is_empty() {
        b.to_string()
    } else {
        format!("{}.{}", a, b)
    }
}

/// Byte range of a token stream: first token start to last token end.
fn range(tokens: TokenStream) -> Option<(usize, usize)> {
    let mut first: Option<Span> = None;
    let mut last: Option<Span> = None;
    for t in tokens {
        if first.is_none() {
            first = Some(t.span());
        }
        last = Some(t.span());
    }
    Some((first?.byte_range().start, last?.byte_range().end))
}

/// White space removed, except one space where two words would otherwise
/// run together (`dyn m::Tr`, `*const T`): docs/lang/identity.md, "Rust".
fn compact(s: &str) -> String {
    let word = |c: char| c == '_' || c.is_ascii_alphanumeric() || !c.is_ascii();
    let mut out = String::new();
    let mut space = false;
    for c in s.chars() {
        if c.is_whitespace() {
            space = true;
            continue;
        }
        if space && out.chars().last().map_or(false, word) && word(c) {
            out.push(' ');
        }
        space = false;
        out.push(c);
    }
    out
}

fn has_cfg(attrs: &[syn::Attribute]) -> bool {
    attrs.iter().any(|a| a.path().is_ident("cfg"))
}

const WRAPPERS: [&str; 5] = ["Box", "Rc", "Arc", "Pin", "Cow"];

impl<'a> Ctx<'a> {
    fn text(&self, span: Span) -> String {
        let r = span.byte_range();
        self.src.get(r.start..r.end).unwrap_or("").to_string()
    }

    fn count(&mut self, form: &str) {
        *self.census.entry(form.to_string()).or_insert(0) += 1;
    }

    /// Self type naming of docs/lang/identity.md: named types drop references,
    /// lifetimes and generic arguments; wrappers and other types keep their
    /// text with whitespace removed.
    fn self_type(&self, ty: &syn::Type) -> String {
        match ty {
            syn::Type::Reference(r) => self.self_type(&r.elem),
            syn::Type::Path(p) if p.qself.is_none() => {
                let seg = p.path.segments.last().unwrap();
                let ident = seg.ident.to_string();
                let generic = !matches!(seg.arguments, syn::PathArguments::None);
                if generic && WRAPPERS.contains(&ident.as_str()) {
                    compact(&self.text(ty.span()))
                } else {
                    ident
                }
            }
            _ => compact(&self.text(ty.span())),
        }
    }

    /// Adds a site and returns its index.
    #[allow(clippy::too_many_arguments)]
    fn add(&mut self, pos: &'static str, kind: &'static str, name: String, q: String, owner: &str, parent: Option<usize>, r: Option<(usize, usize)>, cfg: bool) -> Option<usize> {
        let (s, e) = r?;
        self.sites.push(Site { pos, kind, name, q, owner: owner.to_string(), parent, start: s + self.base, end: e + self.base, cfg });
        Some(self.sites.len() - 1)
    }

    /// The scope of the body of a function site `idx` named `q` declared in
    /// `scope`: claimed when the function is.
    fn body_scope(scope: &Scope, idx: Option<usize>, q: &str) -> Scope {
        match (scope, idx) {
            (Scope::Unclaimed(_), _) | (_, None) => Scope::Unclaimed(format!("fn:{}", q)),
            (_, Some(i)) => Scope::Fn { owner: i, base: q.to_string() },
        }
    }

    /// items: a module body (file root or inline module), or the items of a
    /// block module (`Unclaimed`).
    fn items(&mut self, items: &[syn::Item], module: &str, scope: &Scope) {
        for item in items {
            self.item(item, module, scope);
        }
    }

    fn item(&mut self, item: &syn::Item, module: &str, scope: &Scope) {
        let (pos_item, owner, parent) = scope.place("item");
        let local = !matches!(scope, Scope::Module);
        let prefix = scope.prefix(module).to_string();
        let mut it = item.clone();
        macro_rules! strip {
            ($v:expr) => {{
                let cfg = has_cfg(&$v.attrs);
                $v.attrs.clear();
                (range($v.to_token_stream()), cfg)
            }};
        }
        match &mut it {
            syn::Item::Fn(f) => {
                let name = f.sig.ident.to_string();
                let block = f.block.clone();
                let (r, cfg) = strip!(f);
                let q = join(&prefix, &name);
                self.count(if local { "local_fn" } else { "fn" });
                let idx = self.add(pos_item, "function", name.clone(), q.clone(), &owner, parent, r, cfg);
                let body = Self::body_scope(scope, idx, &q);
                self.block(&block, module, &body);
            }
            syn::Item::Struct(s) => {
                let name = s.ident.to_string();
                let (r, cfg) = strip!(s);
                self.count("struct");
                self.add(pos_item, "struct", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
            }
            syn::Item::Enum(s) => {
                let name = s.ident.to_string();
                let (r, cfg) = strip!(s);
                self.count("enum");
                self.add(pos_item, "enum", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
            }
            syn::Item::Union(s) => {
                let name = s.ident.to_string();
                let (r, cfg) = strip!(s);
                self.count("union");
                self.add(pos_item, "union", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
            }
            syn::Item::Type(s) => {
                let name = s.ident.to_string();
                let (r, cfg) = strip!(s);
                self.count("type_alias");
                self.add(pos_item, "type_alias", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
            }
            syn::Item::Const(s) => {
                let name = s.ident.to_string();
                let public = !matches!(s.vis, syn::Visibility::Inherited);
                self.count(if public { "const_pub" } else { "const_private" });
                let expr = s.expr.clone();
                let (r, cfg) = strip!(s);
                if public && name != "_" {
                    self.add(pos_item, "constant", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
                }
                self.expr_items(&expr, module, &Scope::Unclaimed(format!("const:{}", join(&prefix, &name))));
            }
            syn::Item::Static(s) => {
                let name = s.ident.to_string();
                let public = !matches!(s.vis, syn::Visibility::Inherited);
                self.count(if public { "static_pub" } else { "static_private" });
                let expr = s.expr.clone();
                let (r, cfg) = strip!(s);
                if public {
                    self.add(pos_item, "variable", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
                }
                self.expr_items(&expr, module, &Scope::Unclaimed(format!("static:{}", join(&prefix, &name))));
            }
            syn::Item::Trait(t) => {
                let name = t.ident.to_string();
                let items = t.items.clone();
                let (r, cfg) = strip!(t);
                let q = join(&prefix, &name);
                self.count("trait");
                let tidx = self.add(pos_item, "trait", name.clone(), q.clone(), &owner, parent, r, cfg);
                let towner = if local { owner.clone() } else { format!("trait:{}", q) };
                // A trait's methods are contained by the trait (row RS-D6,
                // and RS-D10 inside a function body).
                let (tpos, tparent) = match scope {
                    Scope::Module => ("trait_fn", tidx),
                    Scope::Fn { .. } if tidx.is_some() => ("fn_item", tidx),
                    _ => ("local", None),
                };
                for ti in &items {
                    let mut ti = ti.clone();
                    match &mut ti {
                        syn::TraitItem::Fn(f) => {
                            let fname = f.sig.ident.to_string();
                            let block = f.default.clone();
                            let (r, cfg) = strip!(f);
                            self.count(if block.is_some() { "trait_fn_default" } else { "trait_fn_required" });
                            let mq = join(&q, &fname);
                            let midx = self.add(tpos, "method", fname, mq.clone(), &towner, tparent, r, cfg);
                            if let Some(b) = block {
                                let body = if tpos == "local" { Scope::Unclaimed(format!("fn:{}", mq)) } else { Self::body_scope(scope, midx, &mq) };
                                self.block(&b, module, &body);
                            }
                        }
                        syn::TraitItem::Const(_) => self.count("trait_const"),
                        syn::TraitItem::Type(_) => self.count("trait_type"),
                        _ => self.count("trait_other"),
                    }
                }
            }
            syn::Item::Impl(im) => {
                let typ = self.self_type(&im.self_ty);
                let tr = im.trait_.as_ref().map(|(_, p, _)| p.segments.last().map(|s| s.ident.to_string()).unwrap_or_default());
                self.count(if tr.is_some() { "impl_trait" } else { "impl_inherent" });
                let items = im.items.clone();
                for ii in &items {
                    let mut ii = ii.clone();
                    match &mut ii {
                        syn::ImplItem::Fn(f) => {
                            let fname = f.sig.ident.to_string();
                            let block = f.block.clone();
                            let (r, cfg) = strip!(f);
                            let mut mq = join(&prefix, &format!("{}.{}", typ, fname));
                            if let Some(t) = &tr {
                                mq = format!("{}@{}", mq, t);
                            }
                            self.count("impl_fn");
                            let (pos, _, _) = scope.place("impl_fn");
                            let midx = self.add(pos, "method", fname, mq.clone(), &owner, parent, r, cfg);
                            let body = Self::body_scope(scope, midx, &mq);
                            self.block(&block, module, &body);
                        }
                        syn::ImplItem::Const(c) => {
                            let public = !matches!(c.vis, syn::Visibility::Inherited);
                            let cname = c.ident.to_string();
                            let expr = c.expr.clone();
                            let (r, cfg) = strip!(c);
                            self.count(if public { "impl_const_pub" } else { "impl_const_private" });
                            let mut aq = join(&prefix, &format!("{}.{}", typ, cname));
                            if let Some(t) = &tr {
                                aq = format!("{}@{}", aq, t);
                            }
                            // Row RS-D9: a member of the self type, as a
                            // method; `const _` (rustc: needs a name) is not.
                            let (mut pos, _, _) = scope.place("impl_assoc");
                            if cname == "_" && pos != "local" {
                                pos = "impl_assoc_unnamed";
                            }
                            self.add(pos, "constant", cname.clone(), aq.clone(), &owner, parent, r, cfg);
                            self.expr_items(&expr, module, &Scope::Unclaimed(format!("const:{}", aq)));
                        }
                        syn::ImplItem::Type(t) => {
                            let tname = t.ident.to_string();
                            let (r, cfg) = strip!(t);
                            self.count("impl_type");
                            let mut aq = join(&prefix, &format!("{}.{}", typ, tname));
                            if let Some(t) = &tr {
                                aq = format!("{}@{}", aq, t);
                            }
                            let (pos, _, _) = scope.place("impl_assoc");
                            self.add(pos, "type_alias", tname.clone(), aq, &owner, parent, r, cfg);
                        }
                        syn::ImplItem::Macro(_) => self.count("impl_macro_invocation"),
                        _ => self.count("impl_other"),
                    }
                }
            }
            syn::Item::Mod(m) => {
                let name = m.ident.to_string();
                if m.attrs.iter().any(|a| a.path().is_ident("path")) {
                    self.count(if m.content.is_some() { "mod_path_attr_inline" } else { "mod_path_attr" });
                }
                if has_cfg(&m.attrs) {
                    self.count("mod_cfg");
                }
                match &m.content {
                    Some((_, body)) => {
                        self.count(if local { "mod_inline_in_block" } else { "mod_inline" });
                        let body = body.clone();
                        // A block's mod item is no module of the file (row
                        // RS-M5): its items claim nothing.
                        let inner = if local { Scope::Unclaimed(format!("mod:{}", join(&prefix, &name))) } else { Scope::Module };
                        self.items(&body, &join(module, &name), &inner);
                    }
                    None => {
                        self.count(if local { "mod_outlined_in_block" } else { "mod_outlined" });
                        let (r, cfg) = strip!(m);
                        let pos = if local { "mod_decl_local" } else { "mod_decl" };
                        self.add(pos, "mod", name.clone(), join(module, &name), &owner, None, r, cfg);
                    }
                }
            }
            syn::Item::Macro(m) => {
                if m.mac.path.is_ident("macro_rules") {
                    if let Some(ident) = &m.ident {
                        let name = ident.to_string();
                        let (r, cfg) = strip!(m);
                        self.count("macro_rules");
                        self.add(pos_item, "macro", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
                    }
                } else {
                    let p = m.mac.path.segments.last().map(|s| s.ident.to_string()).unwrap_or_default();
                    if p == "include" {
                        self.count("include_macro");
                    } else {
                        self.count("item_macro_invocation");
                    }
                }
            }
            syn::Item::ForeignMod(fm) => {
                self.count("extern_block");
                let items = fm.items.clone();
                for fi in &items {
                    let mut fi = fi.clone();
                    match &mut fi {
                        syn::ForeignItem::Fn(f) => {
                            let name = f.sig.ident.to_string();
                            let (r, cfg) = strip!(f);
                            self.count("foreign_fn");
                            let (pos, _, _) = scope.place("foreign_fn");
                            self.add(pos, "function", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
                        }
                        syn::ForeignItem::Static(s) => {
                            let name = s.ident.to_string();
                            let public = !matches!(s.vis, syn::Visibility::Inherited);
                            let (r, cfg) = strip!(s);
                            self.count("foreign_static");
                            if public {
                                let (pos, _, _) = scope.place("foreign_static");
                                self.add(pos, "variable", name.clone(), join(&prefix, &name), &owner, parent, r, cfg);
                            }
                        }
                        _ => self.count("foreign_other"),
                    }
                }
            }
            syn::Item::Use(u) => {
                self.count(if local { "use_in_block" } else { "use" });
                let (r, cfg) = strip!(u);
                let pos = if local { "use_decl_local" } else { "use_decl" };
                self.add(pos, "use", String::new(), module.to_string(), &owner, None, r, cfg);
            }
            syn::Item::ExternCrate(e) => self.count(if e.rename.is_some() { "extern_crate_as" } else { "extern_crate" }),
            syn::Item::TraitAlias(_) => self.count("trait_alias"),
            _ => self.count("item_other"),
        }
    }

    fn block(&mut self, b: &syn::Block, module: &str, scope: &Scope) {
        let mut v = BlockItems { ctx: self, module: module.to_string(), scope: scope.clone() };
        syn::visit::Visit::visit_block(&mut v, b);
    }

    fn expr_items(&mut self, e: &syn::Expr, module: &str, scope: &Scope) {
        let mut v = BlockItems { ctx: self, module: module.to_string(), scope: scope.clone() };
        syn::visit::Visit::visit_expr(&mut v, e);
    }
}

/// Collects the items nested in a function body or another expression, at
/// any depth (blocks, closures, match arms), in the given scope; nested
/// items handle their own bodies.
struct BlockItems<'c, 'a> {
    ctx: &'c mut Ctx<'a>,
    module: String,
    scope: Scope,
}

impl<'ast, 'c, 'a> syn::visit::Visit<'ast> for BlockItems<'c, 'a> {
    fn visit_item(&mut self, i: &'ast syn::Item) {
        let scope = self.scope.clone();
        let module = self.module.clone();
        self.ctx.item(i, &module, &scope);
    }
}

fn line_col(src: &[u8], off: usize) -> (usize, usize) {
    let before = &src[..off.min(src.len())];
    let line = before.iter().filter(|&&b| b == b'\n').count() + 1;
    let col = match before.iter().rposition(|&b| b == b'\n') {
        Some(i) => off - i - 1,
        None => off,
    };
    (line, col)
}

fn main() {
    let root = std::env::args().nth(1).expect("usage: rust-structure-oracle <root> < files");
    let stdin = io::stdin();
    let stdout = io::stdout();
    let mut out = io::BufWriter::new(stdout.lock());
    for line in stdin.lock().lines() {
        let rel = line.expect("reading stdin");
        if rel.is_empty() {
            continue;
        }
        proc_macro2::extra::invalidate_current_thread_spans();
        let raw = match std::fs::read(std::path::Path::new(&root).join(&rel)) {
            Ok(b) => b,
            Err(e) => {
                writeln!(out, "{{\"file\":{},\"ok\":false,\"error\":{}}}", json_str(&rel), json_str(&e.to_string())).unwrap();
                continue;
            }
        };
        let mut flags = Vec::new();
        if raw.iter().enumerate().any(|(i, &b)| b == b'\r' && raw.get(i + 1) != Some(&b'\n')) {
            flags.push("lone_cr");
        }
        if raw.windows(2).any(|w| w == b"\r\n") {
            flags.push("crlf");
        }
        let text = match std::str::from_utf8(&raw) {
            Ok(t) => t,
            Err(e) => {
                writeln!(out, "{{\"file\":{},\"ok\":false,\"error\":{}}}", json_str(&rel), json_str(&format!("utf8: {}", e))).unwrap();
                continue;
            }
        };
        let mut base = 0;
        let mut body = text;
        if let Some(rest) = body.strip_prefix('\u{feff}') {
            base = 3;
            body = rest;
            flags.push("bom");
        }
        // A shebang line (not an inner attribute) is blanked, keeping offsets.
        let owned;
        if body.starts_with("#!") && !body.trim_start_matches("#!").trim_start().starts_with('[') {
            let end = body.find('\n').unwrap_or(body.len());
            owned = format!("{}{}", " ".repeat(body[..end].len()), &body[end..]);
            body = &owned;
            flags.push("shebang");
        }
        let flags_json: Vec<String> = flags.iter().map(|f| json_str(f)).collect();
        match syn::parse_str::<syn::File>(body) {
            Err(e) => {
                let lc = e.span().start();
                writeln!(out, "{{\"file\":{},\"ok\":false,\"flags\":[{}],\"error\":{}}}", json_str(&rel), flags_json.join(","), json_str(&format!("{}:{}: {}", lc.line, lc.column, e))).unwrap();
            }
            Ok(file) => {
                let mut ctx = Ctx { src: body, base, sites: Vec::new(), census: Default::default() };
                ctx.items(&file.items, "", &Scope::Module);
                let mut s = String::new();
                for (i, site) in ctx.sites.iter().enumerate() {
                    let (sl, sc) = line_col(&raw, site.start);
                    let (el, ec) = line_col(&raw, site.end);
                    if i > 0 {
                        s.push(',');
                    }
                    let _ = write!(
                        s,
                        "{{\"pos\":{},\"kind\":{},\"name\":{},\"q\":{},\"owner\":{},\"parent\":{},\"start\":{},\"end\":{},\"span\":[{},{},{},{}],\"cfg\":{}}}",
                        json_str(site.pos),
                        json_str(site.kind),
                        json_str(&site.name),
                        json_str(&site.q),
                        json_str(&site.owner),
                        site.parent.map_or(-1, |p| p as i64),
                        site.start,
                        site.end,
                        sl,
                        sc,
                        el,
                        ec,
                        site.cfg
                    );
                }
                let census: Vec<String> = ctx.census.iter().map(|(k, v)| format!("{}:{}", json_str(k), v)).collect();
                writeln!(out, "{{\"file\":{},\"ok\":true,\"flags\":[{}],\"sites\":[{}],\"census\":{{{}}}}}", json_str(&rel), flags_json.join(","), s, census.join(",")).unwrap();
            }
        }
    }
}
