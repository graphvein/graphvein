const { Sequelize, DataTypes } = require('sequelize');

// A global define (underscored and paranoid, investigation probe 2) read by every
// model of the instance.
const sequelize = new Sequelize({
  dialect: 'postgres',
  database: 'fixture',
  logging: false,
  define: { underscored: true, paranoid: true },
});

// A module-level define of the instance's own module (row SQ-M1):
// BlogPost -> blog_posts, the generated index name is
// lower-cased.
const BlogPost = sequelize.define(
  'BlogPost',
  {
    readCount: { type: DataTypes.INTEGER, allowNull: false, defaultValue: 0 },
    title: DataTypes.STRING,
    body: DataTypes.TEXT,
  },
  { indexes: [{ fields: ['read_count', 'title'] }] },
);

module.exports = { sequelize, BlogPost };
