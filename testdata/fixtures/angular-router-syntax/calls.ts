import { NgModule } from '@angular/core';
import { provideRouter, provideRouter as configure, RouterModule } from '@angular/router';
import * as router from 'other-router';
import { platformBrowserDynamic } from '@angular/platform-browser-dynamic';
import { routes, M } from './targets';

provideRouter(routes);
configure(routes, feature());
router.provideRouter(routes);
const root = RouterModule.forRoot(routes, { useHash: true });
const child = router.RouterModule.forChild(routes);
@NgModule({ imports: [RouterModule.forRoot(routes), RouterModule.forChild(routes)] })
export class AppModule {}
platformBrowserDynamic().bootstrapModule(M).catch(error => report(error));
platform.bootstrapModule();
const x = { forRoot() {} };
x.forRoot();

function parameterShadow(routes: unknown) { provideRouter(routes); }
function blockShadow() {
  { const routes = []; RouterModule.forChild(routes); }
}
function calleeShadow(RouterModule: unknown) { RouterModule.forRoot(routes); }
function loopShadow() {
  for (const routes of lists) { configure(routes); }
  try {} catch (routes) { configure(routes); }
}
function defaultExpression(unused = routes) { configure(routes); }
function destructuredDefault({ unused = routes }) { configure(routes); }
function decoratorShadow(routes: unknown) {
  @NgModule({ imports: [RouterModule.forChild(routes)] })
  class Local {}
}
function inlineShadow() {
  class M {}
  provideRouter([{ path: 'inline', component: M, canActivate: [routes] }]);
}
provideRouter([{ path: 'module', component: M }]);
