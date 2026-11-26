import { defineConfig } from 'orval';
export default defineConfig({
  "fetch": {
    "input": "../openapi.yaml",
    "output": {
      "client": "react-query",
      "httpClient": "fetch",
      "mode": "single",
      "workspace": "./api",
      "target": "./generated",
      "baseUrl": "http://fixture",
      "override": {
        "mutator": {
          "path": "../transport.ts",
          "name": "apiFetch"
        },
        "query": {
          "useQuery": true,
          "useMutation": false,
          "useSuspenseQuery": true
        }
      }
    }
  }
});
