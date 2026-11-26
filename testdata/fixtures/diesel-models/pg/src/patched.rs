// @generated automatically by Diesel CLI.

pub mod sql_types {
    #[derive(diesel::query_builder::QueryId, Clone, diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "my_kind"))]
    pub struct UserKind;
}

diesel::table! {
    use diesel::sql_types::*;
    use super::sql_types::UserKind;

    patched_users (id) {
        id -> Int4,
        #[max_length = 10]
        name -> Varchar,
        kind -> UserKind,
        note -> Nullable<Text>,
    }
}
