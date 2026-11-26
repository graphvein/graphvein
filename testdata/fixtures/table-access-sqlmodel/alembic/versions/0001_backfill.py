"""An Alembic-like data migration: ordinary Python (W6)."""
import sqlalchemy as sa
from alembic import op

revision = "0001"
down_revision = None


def upgrade():
    conn = op.get_bind()
    users = sa.table("user", sa.column("name"))
    conn.execute(users.update().values(name="x"))
    conn.execute(sa.text("UPDATE teams SET name = 'x'"))
    op.execute("UPDATE item SET title = ''")


def downgrade():
    op.add_column("item", sa.Column("note", sa.String(), server_default=sa.text("''")))
