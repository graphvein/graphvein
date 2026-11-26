// A schema file no diesel.toml names (DI-U2): names only.

diesel::table! {
    background_jobs (id) {
        id -> Int8,
        job_type -> Text,
        created_at -> Timestamp,
    }
}
