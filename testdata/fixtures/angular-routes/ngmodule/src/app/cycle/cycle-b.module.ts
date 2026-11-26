import { NgModule } from '@angular/core';
import { RouterModule } from '@angular/router';

@NgModule({
  imports: [RouterModule.forChild([{ path: 'a', loadChildren: () => import('./cycle-a.module').then(m => m.CycleAModule) }])],
})
export class CycleBModule {}
