// A wrapper whose result is the action (row SA-M1, form const_call).
export function validated<T>(fn: (data: FormData) => Promise<T>) {
  return async (data: FormData) => fn(data);
}
