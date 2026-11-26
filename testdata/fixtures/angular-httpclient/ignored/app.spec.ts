import { provideHttpClient, withInterceptors } from '@angular/common/http';
const prefix = (req, next) => next(req.clone({url: `/wrong${req.url}`}));
provideHttpClient(withInterceptors([prefix]));
