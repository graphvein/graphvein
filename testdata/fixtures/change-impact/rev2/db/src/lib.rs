// sqlx queries on the migration tables (T019 Revision 4, I8).
use sqlx::PgPool;

pub async fn item_name(pool: &PgPool, id: i32) -> sqlx::Result<String> {
    sqlx::query_scalar!("SELECT name FROM items WHERE id = $1", id)
        .fetch_one(pool)
        .await
}

pub async fn rename_item(pool: &PgPool, id: i32, name: &str) -> sqlx::Result<()> {
    sqlx::query("UPDATE items SET name = $1 WHERE id = $2")
        .bind(name)
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

pub fn all_items() -> impl Sized {
    sqlx::query!("SELECT id FROM items")
}

pub async fn count_rows(pool: &PgPool) -> sqlx::Result<Option<i64>> {
    sqlx::query_scalar!("SELECT count(*) FROM items")
        .fetch_one(pool)
        .await
}
