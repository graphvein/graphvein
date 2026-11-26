// A class model whose base comes from a project module (row G6, init).
const { BaseModel } = require("./base");

class Legacy extends BaseModel {}

Legacy.init({ code: { type: "VARCHAR(3)" } }, { sequelize: global.db, modelName: "Legacy" });

module.exports = Legacy;
