import { defineConfig } from 'orval';
export default defineConfig({
  "fetch": {
    "input": "../openapi.yaml",
    "output": {
      "client": "react-query",
      "httpClient": "fetch",
      "mode": "tags-split",
      "workspace": "./api",
      "target": "./generated",
      "baseUrl": "/t012e",
      "override": {
        "query": {
          "useQuery": true,
          "useMutation": false,
          "useSuspenseQuery": true
        }
      }
    }
  }
});
