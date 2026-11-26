// A sqlx package without a migration set: no_schema_source.
pub async fn lookup(pool: &sqlx::PgPool) -> sqlx::Result<()> {
    sqlx::query!("select id from accounts").fetch_all(pool).await?;
    Ok(())
}
