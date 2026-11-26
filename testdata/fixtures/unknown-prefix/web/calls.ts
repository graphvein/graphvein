export function full(id: string, org: string, tenant: string) {
  fetch(`/v1/upb/orders/${id}`);
  fetch('/upb/orders', {method: 'POST'});
  fetch('/v1/upbping');
  fetch('/v1/upb/shared/list');
  fetch('/v1/upb/stats/daily');
  fetch('/v1/acme/upb/settings');
  fetch(`/v1/${org}/upb/settings`);
  fetch(`/${tenant}/upb/orders/${id}`);
  fetch('/v1/upb/files');
  fetch('/v1/upb/files/');
  fetch('/k/upb/known/items');
  fetch('/z/upb/known/items');
  fetch(`/v1/upb/reports/${id}`, {method: 'HEAD'});
  fetch(`/v1/upb/orders/${id}`, {method: 'HEAD'});
  fetch('/v1/upb/testonly/items');
  fetch(`/outer/v2/upb/inner/${id}`);
  fetch(`/v1/upb/orders/${id}`, {method: 'DELETE'});
}
export function bases(id: string) {
  fetch(`${import.meta.env.VITE_API}/upb/orders/${id}`);
  fetch(`${import.meta.env.VITE_API}/v1/upb/orders/${id}`);
  fetch(`${import.meta.env.VITE_API}/outer/upb/inner/${id}`);
  fetch(`https://api.example.com/v1/upb/orders/${id}`);
}
export function fallback() {
  fetch('/k/app/upb/widgets/list');
  fetch('/k/app/other/page');
  fetch('/k/app/daily');
}
