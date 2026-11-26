import { Component, NgModule } from '@angular/core';
import { RouterModule } from '@angular/router';

@Component({ selector: 'app-exported', template: '' })
export class ExportedComponent {}

@NgModule({
  declarations: [ExportedComponent],
  imports: [RouterModule.forChild([{ path: 'exported', component: ExportedComponent }])],
})
export class ExportedRoutingModule {}

@NgModule({ exports: [ExportedRoutingModule] })
export class ExportedModule {}
