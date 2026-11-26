import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
const prefix = (req, next) => next(req.clone({ url: unknownPrefix() + req.url }));
provideHttpClient(withInterceptors([prefix]));
class Api {
  constructor(private http: HttpClient) {}
  run() { this.http.get('/t012b-r3-prefix'); }
}
