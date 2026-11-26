const pageConfig = JSON.parse(document.querySelector("script#config")?.textContent || "{}");
const config = { ...pageConfig };
export const withBase = (path: string) => `${config.base}${path}`;
