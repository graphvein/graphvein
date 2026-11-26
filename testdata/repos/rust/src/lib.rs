mod worker;
mod nested;
mod broken;
#[path = "elsewhere.rs"] mod custom;
use crate::worker::{work as task, User};
use crate::nested::*;
use std::fmt::Display;
use other_crate::remote;

pub const ANSWER: usize = 42;
const PRIVATE: usize = 0;
pub static READY: bool = true;
pub union Value { pub n: i32, pub f: f32 }
pub enum State { Ready, Done }
pub type Alias = State;
macro_rules! greeting { () => { "hello" } }

pub trait Local {
    fn save(&self) { helper(); }
}
pub fn helper() {}
pub fn run() {
    task();
    worker::work();
    crate::nested::deep();
    User::new();
    inline::go();
    remote();
    println!("hello");
    missing();
}
mod inline {
    use super::helper;
    pub fn go() { helper(); self::again(); }
    fn again() {}
}
#[cfg(test)]
mod tests {
    use super::run;
    fn check() { run(); }
}
#[cfg(feature = "a")]
fn alternative() {}
#[cfg(not(feature = "a"))]
fn alternative() {}
