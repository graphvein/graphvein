// @generated automatically by Diesel CLI.

pub mod sql_types {
    #[derive(diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "mood"))]
    pub struct Mood;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "ltree", schema = "ext"))]
    pub struct Ltree;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(postgres_type(oid = 600, array_oid = 1017))]
    pub struct Point;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "citext"))]
    pub struct Ci;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(sqlite_type(name = "Text"))]
    pub struct Lite;
}

diesel::table! {
    use diesel::sql_types::*;
    use super::sql_types::*;

    /// Every builtin type of row DI-C2.
    scalars (id) {
        id -> Int4,
        small -> Int2,
        big -> Nullable<Int8>,
        f4 -> Float4,
        f8 -> Float8,
        flag -> Bool,
        body -> Text,
        vc -> Varchar,
        #[max_length = 5]
        vc5 -> Varchar,
        #[max_length = 3]
        c3 -> Bpchar,
        c -> Bpchar,
        ch -> Char,
        raw -> Bytea,
        day -> Date,
        at -> Time,
        ts -> Timestamp,
        tstz -> Timestamptz,
        span -> Interval,
        amount -> Numeric,
        j -> Json,
        jb -> Jsonb,
        u -> Uuid,
        o -> Oid,
        m -> Money,
        ip -> Inet,
        net -> Cidr,
        mac -> Macaddr,
        mac8 -> Macaddr8,
        ci -> Citext,
        tv -> Tsvector,
        tq -> Tsquery,
        #[max_length = 3]
        bits -> Bit,
        bit1 -> Bit,
        vb -> Varbit,
        #[max_length = 8]
        vb8 -> Varbit,
        x -> Xml,
        r4 -> Int4range,
        r8 -> Int8range,
        dr -> Daterange,
        nr -> Numrange,
        tr -> Tsrange,
        tzr -> Tstzrange,
        lsn -> PgLsn,
        tags -> Array<Text>,
        nums -> Nullable<Array<Nullable<Int4>>>,
        names -> Array<Varchar>,
        mood -> Mood,
        moods -> Array<Mood>,
        path -> Ltree,
        pt -> Point,
        cie -> Ci,
        lite -> Lite,
        rng -> Range<Int4>,
        q -> diesel::sql_types::Text,
        bogus -> Whatever,
        qn -> diesel::sql_types::Nullable<Int4>,
        #[sql_name = "Weird Name"]
        weird -> Text,
        r#type -> Text,
        #[max_length = 3]
        badlen -> Int4,
    }
}

diesel::table! {
    use diesel::sql_types::*;

    users (id) {
        id -> Int4,
        name -> Text,
        email -> Nullable<Varchar>,
    }
}

diesel::table! {
    use diesel::sql_types::*;

    posts (id) {
        id -> Int4,
        author_id -> Int4,
        editor_id -> Nullable<Int4>,
        title -> Text,
    }
}

diesel::table! {
    pairs (a, b) {
        a -> Int4,
        b -> Int4,
        user_id -> Int4,
    }
}

diesel::table! {
    nokey () {
        id -> Int4,
        val -> Text,
    }
}

diesel::table! {
    #[sql_name = "order_lines"]
    lines (id) {
        id -> Int4,
        post_id -> Int4,
    }
}

diesel::table! {
    legacy (id) {
        id -> Int4,
        kept -> Text,
    }
}

table! {
    tags (id) {
        id -> Int4,
        label -> Text,
        draft -> Bool,
    }
}

::diesel::table! {
    notes (id) {
        id -> Int4,
        post_id -> Int4,
    }
}

diesel::table! {
    r#match (id) {
        id -> Int4,
    }
}

diesel::table! {
    #[allow(non_camel_case_types)]
    odd (id) {
        id -> Int4,
    }
}

diesel::table! {
    broken_key (nope) {
        id -> Int4,
    }
}

diesel::table! {
    extras (id) {
        id -> Int4,
        #[allow(dead_code)]
        attr -> Text,
    }
}

diesel::table! {
    /// A view declared as a table.
    view_counts (post_id) {
        post_id -> Int4,
        n -> Int8,
    }
}

diesel::joinable!(posts -> users (author_id));
diesel::joinable!(posts -> users (editor_id));
joinable!(notes -> posts (post_id));
diesel::joinable!(lines -> posts (post_id));
diesel::joinable!(pairs -> users (user_id));
diesel::joinable!(posts -> pairs (id));
diesel::joinable!(crate::schema::legacy -> users (id));
diesel::joinable!(legacy -> users (id));
diesel::joinable!(view_counts -> posts (post_id));

diesel::allow_tables_to_appear_in_same_query!(scalars, users, posts, pairs, nokey, lines, legacy, tags, notes);

diesel::define_sql_function!(fn lower(x: diesel::sql_types::Text) -> diesel::sql_types::Text);
