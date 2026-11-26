import { Component, Injectable, NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';
import { externalGuard } from 'guards-lib';
import { HelpModule } from '../help.module';

@Component({ selector: 'app-settings', template: '' })
export class SettingsComponent {}

@Injectable()
export class SettingsGuard {}

@Injectable()
export class SettingsResolver {}

const canLeave = () => true;

const settingsRoutes: Routes = [
  {
    path: '',
    component: SettingsComponent,
    canActivate: [SettingsGuard, externalGuard, () => true],
    canDeactivate: [canLeave],
    resolve: { settings: SettingsResolver },
  },
  { path: 'profile', loadChildren: () => import('../profile/profile.module').then(m => m.ProfileModule) },
];

@NgModule({
  declarations: [SettingsComponent],
  imports: [HelpModule, RouterModule.forChild(settingsRoutes)],
  providers: [SettingsGuard, SettingsResolver],
})
export class SettingsModule {}
