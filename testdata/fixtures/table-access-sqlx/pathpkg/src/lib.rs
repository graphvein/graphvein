// A set that creates a schema and changes search_path. unsure may live in
// a "$user" schema: its schema is not proven, so the name is unresolved
// (Revision 2). moved lives in app (the schema front end reads the path),
// so the unqualified name, public.moved under TA-V4, is unresolved and the
// qualified name is claimed.
pub async fn both(pool: &sqlx::PgPool) -> sqlx::Result<()> {
    sqlx::query!("select id from settled").fetch_all(pool).await?;
    sqlx::query!("select id from unsure").fetch_all(pool).await?;
    sqlx::query!("select id from moved").fetch_all(pool).await?;
    sqlx::query!("select id from app.moved").fetch_all(pool).await?;
    Ok(())
}
