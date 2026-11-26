import { HttpClient, HTTP_INTERCEPTORS, provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
class Prefix {
  intercept(req, next) { return next.handle(req.clone({url: `/t012b-api${req.url}`})); }
}
export const app = {providers: [provideHttpClient(withInterceptorsFromDi()), {provide: HTTP_INTERCEPTORS, useClass: Prefix, multi: true}]};
class Api {
  constructor(private http: HttpClient) {}
  run() { this.http.get('/t012b-items'); }
}
