import { call } from './ipc'

// A second-level wrapper.
export function relay(name: string) {
  return call<void>(name)
}
