# Rust extraction fixture

One crate covers file/directory/inline modules, grouped/aliased/glob imports,
trait defaults, inherent and trait methods, generic receivers, repeated cfg
declarations, external imports and recovered syntax. No ignore-name mapping is
needed; all paths are inert tracked source inputs. cfg/build conditions are not
evaluated. `#[path]` is deliberately unsupported and diagnosed.
