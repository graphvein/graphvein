import { invoke } from '@tauri-apps/api/core'

// A callable that writes its parameter before forwarding it is not a
// wrapper (Revision 2, m1): Tauri receives the written value. Each inner
// site keeps its own ipc_call with a dynamic name; the calls below are not
// invoke intents.
export function destructured(command: string) {
  ;[command] = ['other']
  return invoke(command)
}

export function objectPattern(command: string) {
  ;({ command } = { command: 'other' })
  return invoke(command)
}

export function updated(command: any) {
  command++
  return invoke(command)
}

export function closure(command: string) {
  const set = () => {
    command += '_x'
  }
  set()
  return invoke(command)
}

export function forOf(command: string, list: string[]) {
  for (command of list) {
    break
  }
  return invoke(command)
}

// Reading the parameter (a log line, a template) keeps the wrapper.
export function logged(command: string) {
  console.log(`invoking ${command}`)
  return invoke(command)
}

export function useWrites() {
  destructured('greet')
  objectPattern('greet')
  updated('greet')
  closure('greet')
  forOf('greet', [])
  return logged('greet')
}
