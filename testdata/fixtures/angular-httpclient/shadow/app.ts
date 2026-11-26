import { HttpClient } from '@angular/common/http';
class Api {
  constructor(private http: HttpClient) {}
  run() { this.http.get('/t012b-api/t012b-items'); }
}
