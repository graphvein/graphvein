// @generated automatically by Diesel CLI.

diesel::table! {
    things (id) {
        id -> Int4,
        label -> Nullable<Text>,
    }
}
