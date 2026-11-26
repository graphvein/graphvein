import { NgModule } from '@angular/core';
import { BrowserModule } from '@angular/platform-browser';
import { AppComponent } from './app.component';
import { AppRoutingModule } from './app-routing.module';
import { HomeModule } from './home.module';
import { HelpModule } from './help.module';
import { ProvidedModule } from './provided/provided.module';
import { ExportedModule } from './exported/exported.module';

@NgModule({
  declarations: [AppComponent],
  imports: [BrowserModule, HomeModule, HelpModule, ProvidedModule, ExportedModule, AppRoutingModule],
  bootstrap: [AppComponent],
})
export class AppModule {}
