import { invoke } from '@tauri-apps/api/core'
import { invoke as run, type invoke as typedInvoke } from '@tauri-apps/api/core'
import * as tauriCore from '@tauri-apps/api/core'
import { core } from '@tauri-apps/api'
import { invoke as legacy } from '@tauri-apps/api/tauri'
import { call } from './lib/ipc'
import { relay } from './lib/relay'

export async function named() {
  await invoke('greet', { name: 'fixture' })
}

export function aliased() {
  return run<string>('bare_cmd')
}

export function namespaced() {
  return tauriCore.invoke('aliased')
}

export function coreMember() {
  return core.invoke('renamed')
}

export function typeOnly() {
  return typedInvoke('greet')
}

export function legacyCall() {
  return legacy('greet')
}

export async function viaWrapper() {
  const value = await call<string>('desktop_only')
  await call(
    'debug_cmd',
  )
  return value
}

export function twoLevel() {
  return relay('test_cmd')
}

export function conditional(flag: boolean) {
  return invoke(flag ? 'greet' : 'renamed')
}

// A parameter forwarded unchanged would make this a wrapper (I2); an
// element of it is a dynamic name.
export function dynamicName(names: string[]) {
  return invoke(names[0])
}

export function unregistered() {
  return invoke('missing_cmd')
}

export function plugins() {
  invoke('plugin:fs|read_file')
  return invoke('plugin:x')
}

export function shadowedWrapper() {
  const call = (name: string) => name
  return call('greet')
}

// Two arms (a cfg-gated one first) may take this name: both are linked.
export function status() {
  return invoke('status')
}
