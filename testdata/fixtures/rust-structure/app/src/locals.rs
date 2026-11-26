//! S4X: items in function bodies (RS-D10), impl associated items (RS-D9),
//! a raw-identifier module (RS-M2).
mod r#impl;
pub use r#impl::Raw;

pub struct S;
pub trait Tr {
    fn d(&self) {
        fn in_default() {}
    }
}
impl S {
    pub const A: u8 = 1;
    type T = u8;
    const WITH_ITEM: u8 = {
        fn in_assoc_init() -> u8 { 0 }
        in_assoc_init()
    };
    pub fn m(&self) {
        fn inner() {
            struct Deep;
        }
        inner();
    }
}
impl Tr for S {
    const B: u8 = 2;
}
impl Iterator for S {
    type Item = u8;
    fn next(&mut self) -> Option<u8> { None }
}

pub fn run() {
    fn helper() {}
    helper();
    struct L;
    enum LE { A }
    union LU { a: u8 }
    type LA = u8;
    impl L {
        fn new() -> Self { L }
        const Z: u8 = 3;
    }
    impl Tr for L {}
    trait LT {
        fn req(&self);
        fn def(&self) {
            fn in_local_default() {}
        }
    }
    let _c = || {
        fn in_closure() {}
    };
    const PRIV: u8 = {
        fn in_const() -> u8 { 1 }
        in_const()
    };
    pub const PUB: u8 = 2;
    pub static PUB_S: u8 = 4;
    extern "C" {
        fn ext();
        pub static ES: i32;
    }
    macro_rules! lm { () => {} }
    if true {
        fn helper() {}
    }
}

pub mod run {
    pub fn helper() {}
}

#[cfg(unix)]
pub fn plat() {
    fn h() {}
}
#[cfg(not(unix))]
pub fn plat() {
    fn h() {}
}

// A macro_rules! in a function body is no module item: `serde` below is
// still the declared dependency (RS-I3), not refused as macro_name.
use serde::Serialize;
pub fn shadow() {
    macro_rules! serde { () => {} }
}

// A method of an impl in a function body: calls never resolve to it
// (unknown, as before S4X).
pub fn caller() {
    impl S {
        fn local_m(&self) {}
    }
    S::local_m(&S);
}

// `mod r#ser;` and the plain spelling `ser` are one name for rustc (an
// ambiguity with the dependency `ser`, E0659): the leaf resolves nothing
// (RS-I5), never the external crate.
mod r#ser;
use ser::Deserialize;
