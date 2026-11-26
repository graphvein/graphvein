import {NgModule, forwardRef as ref} from '@angular/core';
import * as core from '@angular/core';
import {CommonModule} from '@angular/common';
import {HttpClientModule} from '@angular/common/http';
import {RouterModule} from '@angular/router';
import {CLEAN,CHANGED,later} from './barrel';
import {Declared,Attribute,LocalPipe,StandalonePipe,Standalone,Service,Selectorless,JitModule,Plain} from './targets';
import {REASSIGNED,INDEXED,CYCLE_A} from './arrays';
@NgModule({declarations:[[Declared],...[Declared],CLEAN,...CLEAN,ref(()=>Declared),core.forwardRef(function(){return Attribute;}),later((()=>{return LocalPipe;}) as () => typeof LocalPipe),ref((unused)=>Declared),ref((Declared)=>Declared)],imports:[CommonModule,HttpClientModule,RouterModule.forRoot([]),{ngModule:JitModule,providers:[],1:'irrelevant'},Standalone,StandalonePipe],exports:[Declared,Attribute,LocalPipe,CommonModule,RouterModule]})
export class Good {}
class Local { static forRoot() {return {ngModule:Good};} }
@NgModule({declarations:[Standalone,Service,Selectorless,Plain],imports:[Declared,Local.forRoot(),true?Good:JitModule,CHANGED],exports:[Service]})
export class InvalidEntries {}
@NgModule({declarations:()=>Declared,imports:Good})
export class InvalidWhole {}
@ComponentDecorator({})
export class Unrelated {}
@NgModule({declarations:[REASSIGNED,INDEXED]})
export class MutationCases {}
@NgModule({declarations:[CYCLE_A]})
export class Cyclic {}
