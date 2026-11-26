export const registry = [];
registry.push(function () {
  const purge = async (id) => fetch(`/t034-api/t034-items/${id}`, {method: 'DELETE'});
  const Row = () => purge('6');
  return Row;
});
export function Footer() {
  purge('8');
}
