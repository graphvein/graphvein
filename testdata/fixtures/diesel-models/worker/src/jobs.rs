// table! outside a schema.rs: silent D.

diesel::table! {
    temp_ids (id) {
        id -> Int8,
    }
}
