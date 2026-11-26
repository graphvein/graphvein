import { Component, NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

@Component({ selector: 'app-help', template: '' })
export class HelpComponent {}

const helpRoutes: Routes = [{ path: 'help', component: HelpComponent }];

@NgModule({
  declarations: [HelpComponent],
  imports: [RouterModule.forChild(helpRoutes)],
})
export class HelpModule {}
