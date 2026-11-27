// Row RS-F4: tree-sitter-rust 0.24.2 reports an error on the outer
// attributes of struct pattern fields, which Rust allows. The file is read
// with them blanked; every item below is claimed.
pub mod inner;

pub struct Services {
    pub wake: u8,
    #[cfg(unix)]
    pub watch: u8,
    pub intake: u8,
}

pub fn split(services: Services) -> u8 {
    // A statement attribute near the error: blanked in the first round,
    // restored in the second.
    #[cfg(not(unix))]
    let extra = 0;
    #[cfg(unix)]
    let extra = 1;
    let Services {
        wake,
        #[cfg(unix)]
        #[allow(unused_variables)]
        watch,
        intake,
    } = services;
    let text = "#[not an attribute]";
    let _ = text;
    match (wake, intake) {
        (w, i) => {
            fn local_helper() {}
            local_helper();
            w + i + extra
        }
    }
}

pub fn by_field(s: &Services) -> u8 {
    let Services { #[cfg(any())] watch: _w, wake, .. } = s;
    *wake
}

pub fn after() {}
