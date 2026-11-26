import { Injectable } from '@angular/core';
class Local {}
function Inject(value) { return () => {}; }
@Injectable()
export class LocalDecorator { constructor(@Inject('ignored') local: Local) {} }
