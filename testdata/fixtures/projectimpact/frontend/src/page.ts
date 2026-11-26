import { createUser, listUsers } from './api';

// UI code: reaches the backend only through the client calls in api.ts.
export function UsersPage() {
  return listUsers();
}
export function SignUpButton() {
  return createUser();
}
