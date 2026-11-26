import { HttpClient, HttpClientModule, HTTP_INTERCEPTORS } from '@angular/common/http';
import { NgModule } from '@angular/core';
class Prefix {
  intercept(req, next) {
    const rewritten = req.clone({url: `https://class.example/t012b-api${req.url}`});
    return next.handle(rewritten);
  }
}
@NgModule({imports: [HttpClientModule], providers: [{provide: HTTP_INTERCEPTORS, useClass: Prefix, multi: true}]})
class App {}
class Api {
  constructor(private http: HttpClient) {}
  run() { this.http.get('/t012b-items'); }
}
