// A SQLite set: no readable Postgres set, no_schema_source.
pub async fn notes(pool: &sqlx::SqlitePool) -> sqlx::Result<()> {
    sqlx::query!("select id from notes").fetch_all(pool).await?;
    Ok(())
}
