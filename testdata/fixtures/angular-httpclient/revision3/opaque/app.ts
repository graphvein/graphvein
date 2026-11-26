import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
const opaque = (req, next) => next(req.clone({ url: rewrite(req.url) }));
provideHttpClient(withInterceptors([opaque]));
class Api {
  constructor(private http: HttpClient) {}
  run() { this.http.get(enabled ? '/t012b-r3-opaque' : 'https://example.invalid/t012b-r3-opaque'); }
}
