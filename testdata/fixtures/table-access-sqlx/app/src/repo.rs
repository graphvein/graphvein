// CT-S1 through use sqlx::..., CT-S2 functions, roles and execution
// unknowns.
use sqlx::{query, query_as, PgPool, QueryBuilder};

pub struct Repo {
    pool: PgPool,
}

#[derive(sqlx::FromRow)]
pub struct Row {
    pub id: i32,
}

impl Repo {
    pub async fn count(&self) -> sqlx::Result<i64> {
        let row = query!("SELECT count(*) AS n FROM Accounts").fetch_one(&self.pool).await?;
        Ok(row.n.unwrap_or(0))
    }

    pub async fn tagged(&self, tag: &str) -> sqlx::Result<Vec<Row>> {
        query_as!(Row, "select p.id from posts p join post_tags pt on pt.post_id = p.id join tags t on t.id = pt.tag_id where t.name = $1", tag)
            .fetch_all(&self.pool)
            .await
    }

    pub async fn functions(&self) -> sqlx::Result<()> {
        sqlx::query("delete from comments where id = $1").bind(1).execute(&self.pool).await?;
        query_as::<_, Row>("select id from accounts").fetch_all(&self.pool).await?;
        sqlx::query_scalar::<_, i64>("select count(*) from tags").fetch_one(&self.pool).await?;
        sqlx::query_with("update tags set name = $1 where id = $2", args()).execute(&self.pool).await?;
        sqlx::query_as_with::<_, Row, _>("select post_id as id from post_tags", args()).fetch_all(&self.pool).await?;
        Ok(())
    }

    // R5-B1: unquoted user in FROM is Postgres's current_user function, no
    // table; quoted "user" is the table.
    pub async fn keyword_user(&self) -> sqlx::Result<()> {
        query!("SELECT * FROM user").fetch_all(&self.pool).await?;
        query!(r#"SELECT id FROM "user""#).fetch_all(&self.pool).await?;
        Ok(())
    }

    pub async fn not_literal(&self, table: &str) -> sqlx::Result<()> {
        sqlx::query(&format!("delete from {table}")).execute(&self.pool).await?;
        sqlx::query("delete from tags; delete from posts").execute(&self.pool).await?;
        Ok(())
    }
}

fn args() -> sqlx::postgres::PgArguments {
    Default::default()
}

// Built here, executed elsewhere: role builds.
pub fn build_delete() -> sqlx::query::Query<'static, sqlx::Postgres, sqlx::postgres::PgArguments> {
    sqlx::query("delete from comments_archive")
}

// Executes a statement whose table is not traced here.
pub async fn run(pool: &PgPool) -> sqlx::Result<()> {
    build_delete().execute(pool).await?;
    let mut builder = QueryBuilder::new("select id from accounts");
    builder.build().fetch_all(pool).await?;
    Ok(())
}

// A local binding: the site builds, the execution is not traced to it.
pub async fn bound(pool: &PgPool) -> sqlx::Result<()> {
    let q = sqlx::query!("select id from tags");
    q.fetch_all(pool).await?;
    Ok(())
}

mod nested {
    // A use declaration of the parent module does not reach a nested
    // module: query! here is not proven to be sqlx's, so it is no site.
    pub fn shadowed() {
        query!("select id from accounts");
    }
}

// A local function (no node: its site is owned by local_fn): the execution
// around its item does not run its statement, so the site builds; the
// statement the local function returns and local_fn executes stays
// statement_not_traced.
pub async fn local_fn(pool: &PgPool) -> sqlx::Result<()> {
    sqlx::query("delete from tags where id = 0")
        .execute({
            fn pick() -> &'static str {
                let _ = sqlx::query("delete from comments where id = 0");
                "x"
            }
            let _ = pick();
            pool
        })
        .await?;
    fn statement() -> sqlx::query::Query<'static, sqlx::Postgres, sqlx::postgres::PgArguments> {
        sqlx::query("delete from posts where id = 0")
    }
    statement().execute(pool).await?;
    Ok(())
}
