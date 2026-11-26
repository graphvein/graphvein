// A sequelize-cli migration: the folder is found, not read (row SQ-U1).
module.exports = {
  up: async (queryInterface) => queryInterface.createTable('Accounts', {}),
  down: async (queryInterface) => queryInterface.dropTable('Accounts'),
};
