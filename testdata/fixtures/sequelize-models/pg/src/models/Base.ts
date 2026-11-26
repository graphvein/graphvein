import { Model } from 'sequelize';

// A project base class whose chain ends in Sequelize's Model (row SQ-M2).
export abstract class Base extends Model {
  declare createdAt: Date;
}
