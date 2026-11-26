import { invoke } from '@tauri-apps/api/core';

export function sayHi() {
  return invoke('greet', { name: 'x' });
}

export function sayBye() {
  return invoke('farewell');
}
