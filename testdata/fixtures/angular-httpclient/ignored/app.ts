import { HttpClient, HTTP_INTERCEPTORS, provideHttpClient } from '@angular/common/http';
class Prefix {
  intercept(req, next) { return next.handle(req.clone({url: `/wrong${req.url}`})); }
}
export const app = {providers: [provideHttpClient(), {provide: HTTP_INTERCEPTORS, useClass: Prefix, multi: true}]};
class Api {
  constructor(private http: HttpClient) {}
  run() { this.http.get('/t012b-api/t012b-items'); }
}
