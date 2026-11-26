export function fetchItems() {
  return fetch('/api/items');
}

export function fetchItem() {
  return fetch('/api/items/7');
}

export function removeLegacy() {
  return fetch('/api/legacy', { method: 'DELETE' });
}

export function fetchReport() {
  return fetch(window.location.href);
}
