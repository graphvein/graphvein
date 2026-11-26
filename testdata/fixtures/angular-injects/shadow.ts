import { Injectable } from '@angular/core';
import { inject } from './shadow-core';
class Local {}
@Injectable()
export class Shadow { field = inject(Local); }
