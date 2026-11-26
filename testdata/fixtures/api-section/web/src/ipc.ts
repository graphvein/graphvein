import { invoke } from '@tauri-apps/api/core'
import * as tauriCore from '@tauri-apps/api/core'
import { call } from './ipcwrap'

export async function direct() {
  await invoke('t016d_greet', { name: 'fixture' })
}

export function namespaced() {
  return tauriCore.invoke('t016d_items')
}

export function viaWrapper() {
  return call<string>('t016d_items')
}

export function conditional(flag: boolean) {
  return invoke(flag ? 't016d_greet' : 't016d_items')
}

export function partlyUnmatched(flag: boolean) {
  return invoke(flag ? 't016d_greet' : 't016d_nope')
}

export function dynamicName(names: string[]) {
  return invoke(names[0])
}

export function plugin() {
  return invoke('plugin:fs|read_file')
}

export function debugOnly() {
  return invoke('t016d_debug')
}
