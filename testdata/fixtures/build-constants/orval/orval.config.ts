import { defineConfig } from 'orval';
export default defineConfig({
  "fetch": {
    "input": "./openapi.yaml",
    "output": {
      "client": "react-query",
      "httpClient": "fetch",
      "mode": "tags-split",
      "workspace": "./api",
      "target": "./generated",
      "baseUrl": "http://localhost:3000",
      "override": {
        "mutator": {
          "path": "../transport.ts",
          "name": "apiFetch"
        },
        "query": {
          "useQuery": true,
          "useMutation": false,
          "useSuspenseQuery": false
        }
      }
    }
  }
});
