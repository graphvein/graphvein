//! Probe library root.
#![allow(dead_code)]
pub mod net;
mod both;
mod nonmod;
#[path = "custom/place.rs"] pub mod placed;
pub mod inl {
    pub mod deep;
    #[path = "elsewhere.rs"] pub mod moved;
    pub struct InlType;
}
#[path = "pathdir"] mod pd { pub mod x; }
#[cfg(test)] mod tests;
#[cfg(feature = "extra")] pub mod extra;
#[cfg(windows)] mod win;
#[cfg(not(windows))] mod notwin;
pub use net::client::Client;
pub use net::*;
pub use util::helpers::{self as h, Help};
use serde::Serialize;
use self::inl::InlType;
use crate::inl::deep::Deep as D2;
use ::util::Root;
pub use r#match::r#fn;
pub mod r#match { pub fn r#fn() {} }
include!("included.rs");
include!(concat!(env!("OUT_DIR"), "/gen.rs"));
macro_rules! make { ($n:ident) => { pub struct $n; impl $n { pub fn made() {} } }; }
make!(Generated);
#[macro_export]
macro_rules! exported { () => {} }
pub use crate::exported as reexported_macro;
pub struct Wrapper<T>(T);
impl<T> Wrapper<T> { pub fn new(t: T) -> Self { Wrapper(t) } pub const ASSOC: u8 = 1; type Bad = u8; }
impl<T> Iterator for Wrapper<T> { type Item = u8; fn next(&mut self) -> Option<u8> { None } }
impl Iterator for Box<InlType> { type Item = u8; fn next(&mut self) -> Option<u8> { None } }
pub trait Shape { const SIDES: u8; type Out; fn area(&self) -> f64; fn name(&self) -> &str { "s" } }
extern "C" { fn c_func(x: i32) -> i32; static C_STATIC: i32; }
pub enum E { A, B }
pub use E::A;
pub const PUB_C: u8 = 1;
const PRIV_C: u8 = 2;
pub static PUB_S: u8 = 3;
pub type Alias = Wrapper<u8>;
pub union U { a: u8, b: u16 }
pub fn outer() {
    mod fnmod { use std::fmt; pub fn g() {} }
    fn helper() {}
    struct Local;
    use self::fnmod::g;
    g();
}
fn helper() {}
pub fn twice() {}
#[cfg(feature = "extra")] pub fn twice2() {}
#[cfg(feature = "extra")] pub fn cfgdup() {}
#[cfg(not(feature = "extra"))] pub fn cfgdup() {}
extern crate core as kore;
use kore::fmt;
pub mod globbed;
use crate::globbed::Local;
use crate::globbed::Deserializer;
use relay::Thing;
use widget::Widget;
macro_rules! warn_me { () => {} }
pub(crate) use warn_me;
extern "C" { pub static C_PUB: i32; }
const _: () = { struct InConst; };
pub mod bom;
pub mod lonecr;
pub mod crlf;
pub mod alts;
pub mod locals;
