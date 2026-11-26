// @generated automatically by Diesel CLI.

pub mod audit {
    pub mod sql_types {
        #[derive(diesel::sql_types::SqlType)]
        #[diesel(postgres_type(name = "level", schema = "audit"))]
        pub struct Level;

        #[derive(diesel::sql_types::SqlType)]
        #[diesel(postgres_type(name = "tsvector", schema = "pg_catalog"))]
        pub struct Tsvector;
    }

    diesel::table! {
        use diesel::sql_types::*;
        use super::sql_types::Level;
        use super::sql_types::Tsvector;

        audit.events (id) {
            id -> Int8,
            actor_id -> Int8,
            level -> Level,
            search -> Tsvector,
        }
    }

    diesel::table! {
        audit.actors (id) {
            id -> Int8,
            name -> Text,
        }
    }

    diesel::table! {
        #[sql_name = "audit.Old Names"]
        audit.Old_Names (id) {
            id -> Int8,
        }
    }

    diesel::joinable!(events -> actors (actor_id));

    diesel::allow_tables_to_appear_in_same_query!(events, actors);
}
