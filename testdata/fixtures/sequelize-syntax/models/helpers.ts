// Attribute helpers (payload version 3): no Sequelize import, so only row
// G9 puts the file in the payload.
export default function (DataTypes) {
  return {
    currency: { type: DataTypes.STRING(3), allowNull: false },
  };
}

export function amount(types, scale) {
  return { type: types.DECIMAL(10, scale) };
}

function local(types) {
  return { type: types.INTEGER };
}

async function later() {
  return local;
}
