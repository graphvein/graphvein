import { InjectionToken } from '@angular/core';
export class Named {}
export default class DefaultToken {}
export class TestToken {}
export interface Shape {}
export type Alias = Named;
export enum Enumeration { One }
export const TOKEN = new InjectionToken<Shape>('token');
