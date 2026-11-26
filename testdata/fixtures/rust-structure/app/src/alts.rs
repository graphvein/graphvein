//! Later fixes: cfg alternatives of one module name (A1), #[path]
//! files as mod-rs files (B1), comments between #[path] and its item (B2),
//! compacted self types (C7).
#[cfg(unix)]
#[path = "alts/sys_unix.rs"]
pub mod sys;
#[cfg(windows)]
#[path = "alts/sys_windows.rs"]
pub mod sys;
pub mod common;
use sys::Shared;
use sys::Handle;
#[cfg(not(feature = "extra"))]
mod backend {
    pub fn run() {}
}
#[cfg(feature = "extra")]
mod backend;
use backend::run;
use probe_old::sub::X;
use ser::Deserialize;
#[path = "alts/foo/bar.rs"]
mod bar;
use bar::Cx;
#[path = "alts/k1.rs"] // the path still applies
mod commented;
#[path = "alts/k2.rs"]
/// A doc comment after the attribute.
mod documented;
pub mod m {
    pub trait Tr {}
}
impl dyn m::Tr {
    pub fn f(&self) {}
}
