import { Injectable } from '@angular/core';

@Injectable({ providedIn: 'root' })
export class AuthGuard {}

@Injectable({ providedIn: 'root' })
export class UserResolver {}

export const canEdit = () => true;

export function makeGuard() {
  return () => true;
}
