// @generated automatically by Diesel CLI.

diesel::table! {
    logs (rowid) {
        rowid -> Integer,
        message -> Text,
        level -> Nullable<Integer>,
    }
}

diesel::table! {
    posts (id) {
        id -> Integer,
        author_id -> Integer,
        title -> Text,
    }
}

diesel::table! {
    users (id) {
        id -> Nullable<Integer>,
        name -> Text,
        log_id -> Integer,
    }
}

diesel::joinable!(posts -> users (author_id));
diesel::joinable!(users -> logs (log_id));
