import { DataTypes } from 'sequelize';

import sequelize from '../db';
import { Base } from './Base';

// A class through a project base class, a schema option and an ENUM in
// that schema (its type is not compared, so unknown).
class Category extends Base {}

Category.init(
  {
    name: { type: DataTypes.STRING, allowNull: false },
    kind: DataTypes.ENUM('a', 'b'),
  },
  { sequelize, schema: 'catalog' },
);

export default Category;
