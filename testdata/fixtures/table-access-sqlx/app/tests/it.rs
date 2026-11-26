// Test files are read like any code (W6).
#[sqlx::test]
async fn reads_accounts(pool: sqlx::PgPool) {
    sqlx::query!("select email from accounts").fetch_all(&pool).await.unwrap();
}
