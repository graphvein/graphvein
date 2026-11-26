import { NgModule } from '@angular/core';
import { Router } from '@angular/router';
import DefaultToken, { Named as Renamed } from './tokens';
@NgModule({providers: [{provide: Renamed, useClass: DefaultToken}, {provide: Router, useValue: {}}]})
export class Providers {}
