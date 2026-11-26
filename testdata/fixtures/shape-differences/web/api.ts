const json = { 'Content-Type': 'application/json' };

// SD-2, SD-8: `color` is sent, ItemCreate does not declare it.
export function addItem(title: string) {
  return fetch('/w4/items/', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, color: 'red' }),
  });
}

// SD-9: ItemCreate requires `title`, this call does not send it.
export function addUntitled(description: string) {
  return fetch('/w4/items/', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ description }),
  });
}

// SD-10: both sides fully read, no difference.
export function addItemExact(title: string, description: string) {
  return fetch('/w4/items/', {
    method: 'POST',
    headers: { 'content-type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ title, description }),
  });
}

// SD-8 one level down: Address declares only `city`.
export function moveTo(name: string, city: string, zip: string) {
  return fetch('/w4/profile', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, address: { city, zip } }),
  });
}

// D: the body is not a literal object.
export function addDynamic(payload: object) {
  return fetch('/w4/items/', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
}

// D: the headers are not written in the init object.
export function addShared(title: string) {
  return fetch('/w4/items/', {
    method: 'POST',
    headers: json,
    body: JSON.stringify({ title, extra: 1 }),
  });
}

// D: no Content-Type, so fetch sends text/plain.
export function addPlain(title: string) {
  return fetch('/w4/items/', { method: 'POST', body: JSON.stringify({ title, extra: 1 }) });
}

// D: the route reads the raw request body (partial).
export function addNote(text: string) {
  return fetch('/w4/notes', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text, extra: 1 }),
  });
}

// D: an Express body is never complete (the content type is not read).
export function publishArticle(title: string, body: string) {
  return fetch('/w4/articles', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ article: { title, body, draft: true } }),
  });
}

// SD-3, SD-8: `q` is sent, the handler declares only skip and limit.
export function searchItems(q: string) {
  return fetch(`/w4/items/?${new URLSearchParams({ skip: '0', limit: '10', q })}`);
}

// SD-7, SD-8: the handler proves it reads no query parameter.
export function profileWithQuery() {
  return fetch(`/w4/profile?${new URLSearchParams({ full: '1' })}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: 'n' }),
  });
}

// D (no SD-9 for `nick`): without a manifest proving Pydantic 2, a
// nullable field without a default may be optional (Pydantic 1).
export function addPersonV1(n: string) {
  return fetch('/w4/v1/people', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ fullName: n }),
  });
}

// SD-9: pydantic>=2.7 is proven, so `nick` (Optional, no default) is required.
export function addPersonV2(n: string) {
  return fetch('/w4/v2/people', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ fullName: n }),
  });
}

// D (no SD-8 for `userName`): serde accepts the alias, so NewUser's
// names are not exactly its fields.
export function addUser(n: string) {
  return fetch('/w4/x/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ userName: n, age: 3 }),
  });
}

// SD-8: NewTeam has no alias and denies unknown fields.
export function addTeam(n: string) {
  return fetch('/w4/x/teams', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title: n, color: 'red' }),
  });
}
