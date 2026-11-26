// Written by hand: no print-schema header.

infer_schema!("dotenv:DATABASE_URL");

diesel::table! {
    handmade (id) {
        id -> Int4,
        note -> Nullable<Text>,
    }
}
