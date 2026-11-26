async fn names(pool: &sqlx::PgPool) -> sqlx::Result<()> {
    sqlx::query!("SELECT name FROM items").fetch_all(pool).await?;
    sqlx::query!("INSERT INTO items (id, name) VALUES (1, 'a')").execute(pool).await?;
    Ok(())
}

fn main() {}
