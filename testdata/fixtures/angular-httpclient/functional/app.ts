import { HttpClient as Client, HttpRequest, provideHttpClient, withInterceptors } from '@angular/common/http';
import { inject as use, Inject, Injectable } from '@angular/core';
import { environment } from './environment';
export const prefix = (req, next) => {
  const rewritten = req.clone({url: `${environment.api}${req.url}`});
  return next(rewritten);
};
export const config = {providers: [provideHttpClient(withInterceptors([headers, prefix]))]};
@Injectable()
export class Api {
  constructor(private http: Client) {}
  get(path: string) { return this.http.get('/t012b-items/' + path); }
  all(id: string) {
    this.http.get<string>('/t012b-items');
    this.http.delete('/t012b-items');
    this.http.head('/t012b-items');
    this.http.options('/t012b-items');
    this.http.post('/t012b-items', {});
    this.http.put('/t012b-items', {});
    this.http.patch('/t012b-items', {});
    this.http.jsonp('/t012b-items', 'callback');
    this.http.request('GET', '/t012b-items');
    this.http.request(id, '/t012b-items');
    this.http.request(new HttpRequest('GET', '/t012b-items'));
    this.http.get('https://absolute.example/t012b-api/t012b-items');
    this.http.get(`http://${id}/t012b-api/t012b-items`);
  }
}
export class Field {
  private client = use(Client);
  run() { this.client.get('/t012b-items'); }
}
export class Decorated {
  constructor(@Inject(Client) private client: Client) {}
  run() { this.client.get('/t012b-items'); }
}
export function local() {
  const client = use(Client);
  client.get('/t012b-items');
}
export class Wrapper {
  private api = use(Api);
  run() { this.api.get('one'); }
}
export class Fake {
  constructor(private http: Other) {}
  run() { this.http.get('/t012b-items'); }
}
export class Constructed {
  constructor(@Inject(Client) client: Client) { client.get('/t012b-items'); }
}
import * as httpTypes from '@angular/common/http';
export class Namespaced {
  constructor(private client: httpTypes.HttpClient) {}
  run() { this.client.get('/t012b-items'); }
}
import { HttpHeaders } from '@angular/common/http';
function headers(req, next) {
  const values = new HttpHeaders({token: 'x'});
  return next(req.clone({headers: values}));
}
const verb = 'GET';
export class ConstantMethod {
  constructor(private client: Client) {}
  run() { this.client.request(verb, '/t012b-items'); }
}
