import { bootstrapApplication as boot, createApplication } from '@angular/platform-browser';
import * as browser from '@angular/platform-browser';
import { mergeApplicationConfig } from '@angular/core';
import { provideRouter } from '@angular/router';
import { App } from './app/pages/app';
import HomePage from './app/pages/home';
import { aliasedConfig, namespacedConfig, spreadConfig } from './app/forms';
import { mergedConfig } from './app/app.config';

const localConfig = { providers: [...aliasedConfig, [namespacedConfig]] };

boot(App, { providers: [spreadConfig, provideRouter([{ path: 'inline-options', component: HomePage }])] });
browser.createApplication(localConfig);
createApplication(mergeApplicationConfig(mergedConfig, {}));
