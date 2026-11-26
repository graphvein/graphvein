// CT-S1 macros spelt with the sqlx:: path; owners are functions, methods
// and the closure's enclosing function.
mod repo;

use sqlx::PgPool;

pub struct Account {
    pub id: i32,
}

pub async fn get_account(pool: &PgPool, id: i32) -> sqlx::Result<Account> {
    sqlx::query_as!(Account, "select id from accounts where id = $1", id)
        .fetch_one(pool)
        .await
}

pub async fn feed(pool: &PgPool, author: i32) -> sqlx::Result<Vec<i32>> {
    sqlx::query_scalar!(
        r#"SELECT p.id FROM posts p
           JOIN accounts a ON a.id = p.author_id
           LEFT OUTER JOIN post_tags pt ON pt.post_id = p.id, tags t
           WHERE a.id = $1 AND t.id = pt.tag_id"#,
        author
    )
    .fetch_all(pool)
    .await
}

pub async fn tag(pool: &PgPool, name: &str) -> sqlx::Result<i32> {
    sqlx::query_scalar!(
        "insert into tags (name) values ($1) on conflict (name) do update set name = excluded.name returning id",
        name
    )
    .fetch_one(pool)
    .await
}

pub async fn tag_quietly(pool: &PgPool, name: &str) -> sqlx::Result<()> {
    sqlx::query!("insert into tags as t (name) values ($1) on conflict do nothing", name)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn link_tags(pool: &PgPool, post: i32, names: &[String]) -> sqlx::Result<()> {
    sqlx::query!(
        "insert into post_tags (post_id, tag_id) select $1::int, id from tags where name = any($2::text[])",
        post,
        names
    )
    .execute(pool)
    .await?;
    Ok(())
}

pub async fn retitle(pool: &PgPool, title: &str, email: &str) -> sqlx::Result<()> {
    sqlx::query!(
        "update posts set title = $1 from accounts a where a.id = posts.author_id and a.email = $2",
        title,
        email
    )
    .execute(pool)
    .await?;
    Ok(())
}

pub async fn copy_views(pool: &PgPool) -> sqlx::Result<()> {
    // An UPDATE ... FROM itself: posts is written and read.
    sqlx::query_unchecked!("update posts p set views = q.views + 1 from posts q where q.id = p.id")
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn prune(pool: &PgPool, author: i32) -> sqlx::Result<()> {
    sqlx::query!(
        "delete from only comments c using posts p where p.id = c.post_id and p.author_id = $1 returning c.id",
        author
    )
    .fetch_all(pool)
    .await?;
    Ok(())
}

pub async fn lock_rows(pool: &PgPool) -> sqlx::Result<()> {
    let mut tx = pool.begin().await?;
    sqlx::query!("select id from accounts for update").fetch_all(&mut *tx).await?;
    sqlx::query!("select id from posts for no key update").fetch_all(&mut *tx).await?;
    sqlx::query!("select id from tags for share").fetch_all(&mut *tx).await?;
    sqlx::query!("select post_id from post_tags for key share").fetch_all(&mut *tx).await?;
    tx.commit().await
}

pub async fn shapes(pool: &PgPool, ids: &[i32]) -> sqlx::Result<()> {
    // A CTE shadows a table name; a writable CTE writes two tables; a
    // function in FROM is no table; subqueries at any depth.
    sqlx::query!(
        "with recent as (select id from posts order by id desc limit 10) \
         select r.id from recent r join accounts on true"
    )
    .fetch_all(pool)
    .await?;
    sqlx::query_unchecked!(
        "with moved as (delete from comments where post_id = $1 returning id, post_id, body) \
         insert into comments_archive select id, post_id, body from moved",
        ids[0]
    )
    .execute(pool)
    .await?;
    sqlx::query!("select u.id from unnest($1::int[]) as u(id) join posts on posts.id = u.id", ids)
        .fetch_all(pool)
        .await?;
    sqlx::query_scalar_unchecked!(
        "select (select count(*) from comments c where c.post_id = p.id) from posts p \
         where exists (select 1 from tags) and p.id in (select post_id from post_tags)"
    )
    .fetch_all(pool)
    .await?;
    sqlx::query!("insert into audit.events (kind) values ($1)", "shapes")
        .execute(pool)
        .await?;
    sqlx::query_as_unchecked!(Account, r#"select id from "Mixed""#)
        .fetch_all(pool)
        .await?;
    sqlx::query!("select now() as now").fetch_one(pool).await?;
    Ok(())
}

pub async fn refused(pool: &PgPool) -> sqlx::Result<()> {
    // A view, a renamed table's old name, a view next to a table (partial),
    // a statement outside the subset.
    sqlx::query!("select n from post_stats").fetch_all(pool).await?;
    sqlx::query!("select id from drafts").fetch_all(pool).await?;
    sqlx::query!("select p.id from posts p join post_stats s on s.post_id = p.id").fetch_all(pool).await?;
    sqlx::query!("truncate comments_archive").execute(pool).await?;
    Ok(())
}

pub fn spawn(pool: PgPool) {
    tokio::spawn(async move {
        // A closure belongs to its enclosing function.
        let _ = sqlx::query!("delete from tags where id = $1", 1).execute(&pool).await;
    });
}
