import { Component, NgModule } from '@angular/core';
import { RouterModule } from '@angular/router';

@Component({ selector: 'app-lazy-root', template: '' })
export class LazyRootComponent {}

@NgModule({
  declarations: [LazyRootComponent],
  imports: [RouterModule.forRoot([{ path: 'again', component: LazyRootComponent }])],
})
export class LazyRootModule {}
