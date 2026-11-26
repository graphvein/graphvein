// @generated automatically by Diesel CLI.

diesel::table! {
    old_users (id) {
        id -> Int4,
        name -> Varchar,
        bits -> Varbit,
        tags -> Array<Nullable<Varchar>>,
        body -> Text,
    }
}

diesel::table! {
    old_posts (id) {
        id -> Int4,
        user_id -> Int4,
    }
}

diesel::joinable!(old_posts -> old_users (user_id));

diesel::allow_tables_to_appear_in_same_query!(
    old_posts,
    old_users,
);
