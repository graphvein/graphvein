export function main() {
  fetch(`${process.env.API}/nothing`, {method: 'DELETE'});
  fetch(`${process.env.API}/health`);
  fetch(`${process.env.API}/orders/42`);
  fetch(`${process.env.API}/assets/logo.png`);
}
