// @generated automatically by Diesel CLI.

pub mod sql_types {
    #[derive(diesel::sql_types::SqlType)]
    #[diesel(mysql_type(name = "Enum"))]
    pub struct ItemsStateEnum;
}

diesel::table! {
    use diesel::sql_types::*;
    use super::sql_types::ItemsStateEnum;

    items (id) {
        id -> Integer,
        flag -> Bool,
        tiny -> Tinyint,
        big -> Unsigned<Bigint>,
        #[max_length = 64]
        name -> Varchar,
        code -> Varchar,
        price -> Decimal,
        seen -> Nullable<Datetime>,
        body -> Text,
        meta -> Json,
        made -> Year,
        state -> ItemsStateEnum,
        owner_id -> Integer,
    }
}

diesel::table! {
    owners (id) {
        id -> Integer,
        #[max_length = 32]
        login -> Char,
    }
}

diesel::joinable!(items -> owners (owner_id));

diesel::allow_tables_to_appear_in_same_query!(items, owners);
