// A class model with init at module level (rows G4, S3, S4).
import { DataTypes, Model } from "sequelize";
import sequelize from "../lib/sequelize";

export abstract class WithId<T, C> extends Model<T, C> {}

@Table
class Order extends WithId<OrderAttributes, OrderCreation> implements Priced {
  @Column declare id: number;
  static total = 0;
}

Order.init(
  {
    id: { type: DataTypes.INTEGER, primaryKey: true, autoIncrement: true },
    status: { type: DataTypes.ENUM(...Object.values(Status)), defaultValue: Status.NEW },
    label: { type: new DataTypes.VIRTUAL(DataTypes.STRING, ["status"]) },
    pattern: { type: DataTypes.STRING, validate: { is: /^[a-z]+$/i } },
    owner: {
      type: DataTypes.INTEGER,
      references: { model: "Users", key: "id" },
      get() {
        return this.getDataValue("owner");
      },
    },
    flags: DataTypes.ARRAY(DataTypes.STRING),
  },
  {
    sequelize,
    tableName: "Orders",
    paranoid: true,
    hooks: {
      beforeCreate: function stamp(order) {
        order.status = order.status || "NEW";
        return order;
      },
    },
    indexes: [{ fields: ["status", "owner"], unique: true }],
  },
);

Order.belongsTo(Order, { as: "parent", foreignKey: "ParentId" });
Temporal(Order, sequelize);
Order.removeAttribute("id");
exports.count = Order.count = 1;
export default Order;
export class Plain {}
