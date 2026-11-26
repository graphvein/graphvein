import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
@Injectable()
class Api {
  constructor(private http: HttpClient) {}
  get(path: string) { return this.http.get('/t012b-api/t012b-items/' + path); }
}
const app = {providers: [{provide: Api, useValue: {}}]};
class Caller {
  private api = inject(Api);
  run() { this.api.get('one'); }
}
