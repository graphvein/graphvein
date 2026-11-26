import { Component, NgModule } from '@angular/core';
import { provideRouter } from '@angular/router';

@Component({ selector: 'app-provided', template: '' })
export class ProvidedComponent {}

@NgModule({
  declarations: [ProvidedComponent],
  providers: [provideRouter([{ path: 'provided', component: ProvidedComponent }])],
})
export class ProvidedModule {}
