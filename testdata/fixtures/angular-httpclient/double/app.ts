import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
const one = (req, next) => next(req.clone({url: `/one${req.url}`}));
const two = (req, next) => next(req.clone({url: `/two${req.url}`}));
provideHttpClient(withInterceptors([one, two]));
class Api {
  constructor(private http: HttpClient) {}
  run() { this.http.get('/t012b-items'); }
}
