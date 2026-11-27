// Row RS-F2, not RS-F4: a struct pattern field attribute and a real syntax
// error. Blanking the attribute leaves the error, so the file claims
// nothing beyond its file node.
pub struct Pair {
    pub a: u8,
    pub b: u8,
}

pub fn split(p: Pair) -> u8 {
    let Pair { a, #[cfg(unix)] b } = p;
    a + b +
}

pub fn after() {}
