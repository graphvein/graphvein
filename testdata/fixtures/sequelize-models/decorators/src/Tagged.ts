import { DataTypes, Model } from 'sequelize';
import { Table } from 'sequelize-typescript';

import sequelize from './db';

// A decorated class (sequelize-typescript forms are row D: developer
// decision 2026-11-13).
@Table
class Tagged extends Model {}

Tagged.init({ name: DataTypes.STRING }, { sequelize });

export default Tagged;
