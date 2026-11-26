import {Attribute, LocalPipe, Single, ArrayOnly} from './targets';
export const CLEAN = [Attribute, LocalPipe] as const;
export const CHANGED = [Single];
export const REASSIGNED = [ArrayOnly];
export const INDEXED = [ArrayOnly];
export const CYCLE_A = [CYCLE_B];
export const CYCLE_B = [CYCLE_A];
