// @generated automatically by Diesel CLI.

pub mod sql_types {
    #[derive(diesel::query_builder::QueryId, Clone, diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "mood"))]
    #[diesel(enum_type)]
    pub struct Mood;

    #[derive(diesel::query_builder::QueryId, Clone, diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "level", schema = "audit"))]
    #[diesel(enum_type)]
    pub struct Level;

    #[derive(diesel::query_builder::QueryId, Clone, diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "timetz", schema = "pg_catalog"))]
    pub struct Timetz;

    #[derive(diesel::query_builder::QueryId, Clone, diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "shape"))]
    pub struct Shape;
}

pub mod rust_types {
    #[derive(Debug, Clone, Copy, PartialEq, Eq, diesel::deserialize::FromSqlRow, diesel::expression::AsExpression)]
    #[diesel(sql_type = super::sql_types::Mood)]
    pub enum Mood {
        Sad,
        Ok,
    }

    #[derive(Debug, Clone, Copy, PartialEq, Eq, diesel::deserialize::FromSqlRow, diesel::expression::AsExpression)]
    #[diesel(sql_type = super::sql_types::Level)]
    pub enum Level {
        Info,
        Warn,
    }
}

diesel::table! {
    use diesel::sql_types::*;
    use super::sql_types::Mood;
    use super::sql_types::Level;
    use super::sql_types::Timetz;
    use super::sql_types::Shape;

    /// People who answered the survey
    people24 (id) {
        id -> Int4,
        mood -> Mood,
        moods -> Array<Nullable<Mood>>,
        level -> Nullable<Level>,
        opens -> Timetz,
        opens_all -> Array<Nullable<Timetz>>,
        outline -> Nullable<Shape>,
    }
}
