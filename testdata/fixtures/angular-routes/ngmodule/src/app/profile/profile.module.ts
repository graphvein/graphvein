import { Component, NgModule } from '@angular/core';
import { RouterModule } from '@angular/router';
import { AuthGuard } from '../core';

@Component({ selector: 'app-profile', template: '' })
export class ProfileComponent {}

@NgModule({
  declarations: [ProfileComponent],
  imports: [RouterModule.forChild([{ path: ':username', component: ProfileComponent, canActivate: [AuthGuard], children: [{ path: 'favorites', component: ProfileComponent }] }])],
})
export class ProfileModule {}
