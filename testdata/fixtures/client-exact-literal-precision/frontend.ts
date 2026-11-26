export function main(tenant: string) {
  fetch(`${process.env.API}/health`);
  fetch(`${process.env.API}/anything`, {method: 'POST'});
  fetch(`${process.env.API}/profile/t017b-edit`);
  fetch(`${process.env.API}/${tenant}/t017b-reports`);
  fetch(`${process.env.API}/t017b-items`);
  fetch(`${process.env.API}/t017b-users/7/t017b-edit`);
  fetch(`${process.env.API}/account/t017b-settings`);
}

export function resolved() {
  fetch('/health');
  fetch('/anything', {method: 'POST'});
  fetch('/account/t017b-settings');
}
