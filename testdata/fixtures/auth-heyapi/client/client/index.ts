// A stand-in for the bundled client @hey-api/openapi-ts 0.99.0 writes here
// (client/client/*, client/core/*): only the exports client.gen.ts and
// sdk.gen.ts import. The extractor reads none of its bodies.
export type Client = { get: Function; post: Function; delete: Function };
export type ClientOptions = { baseURL?: string };
export type Config<T = ClientOptions> = T;
export type ClientMeta = Record<string, never>;
export type Options<TData = unknown, ThrowOnError extends boolean = boolean, TResponse = unknown> = Record<string, unknown>;
export type RequestResult<TResponses = unknown, TErrors = unknown, ThrowOnError extends boolean = boolean> = Promise<unknown>;
export type TDataShape = Record<string, unknown>;

export function createConfig<T>(override?: T): T {
  return (override ?? {}) as T;
}

export function createClient(config: unknown): Client {
  return config as Client;
}
