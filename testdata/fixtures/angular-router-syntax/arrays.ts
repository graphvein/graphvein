import { Routes } from '@angular/router';
export default (([{ path: '', children: [] }] as Routes) satisfies Routes);
export const checked = ([{ path: 'checked', children: [] }] satisfies Routes);
const ordinary = [{ path: 'ordinary', children: [] }];
let mutable = [] satisfies Routes;
const first = [] satisfies Routes, second = [] satisfies Routes;
function local() { const nested = [] satisfies Routes; }
