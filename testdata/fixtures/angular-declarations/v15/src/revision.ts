import {NgModule} from '@angular/core';
import {UnknownSelector,Conflict} from './decorators';
import {Declared} from './targets';
import {AliasDeclared} from './revision-alias';
const SAT = [Declared] satisfies unknown[];
@NgModule({declarations:[UnknownSelector,AliasDeclared,SAT],imports:[Conflict]})
export class RevisionModule {}
