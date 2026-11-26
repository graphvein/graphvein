import { Component, NgModule } from '@angular/core';
import { RouterModule } from '@angular/router';

@Component({ selector: 'app-orphan', template: '' })
export class OrphanComponent {}

@NgModule({
  declarations: [OrphanComponent],
  imports: [RouterModule.forChild([{ path: 'orphan', component: OrphanComponent }])],
})
export class OrphanModule {}
