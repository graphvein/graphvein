export const createConfig = (config = {}) => config;
export const createClient = (config = {}) => ({
  get: (options) => options,
  post: (options) => options,
  setConfig: (next) => ({ ...config, ...next }),
});
