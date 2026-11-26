// An options module read through the environment selector (row G7).
module.exports = {
  development: {
    dialect: "sqlite",
    storage: process.env.SQLITE_FILE || "./dev.db",
    define: { underscored: true, freezeTableName: true, createdAt: "created_at" },
    retry: { match: [/SQLITE_BUSY/], max: 5 },
  },
  production: {
    dialect: "sqlite",
    define: { underscored: true, freezeTableName: true, createdAt: "created_at" },
  },
};
