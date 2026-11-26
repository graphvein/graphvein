// A stand-in for the bundled client @hey-api/openapi-ts writes here: only
// the exports client.gen.ts and sdk.gen.ts import. The extractor reads none
// of its bodies.
export type Client = { get: Function; post: Function; put: Function };
export type ClientOptions = { baseURL?: string };
export type Config<T = ClientOptions> = T;
export type Options<TData = unknown, ThrowOnError extends boolean = boolean> = Record<string, unknown>;
export type TDataShape = Record<string, unknown>;

export function createConfig<T>(override?: T): T {
  return (override ?? {}) as T;
}

export function createClient(config: unknown): Client {
  return config as Client;
}
