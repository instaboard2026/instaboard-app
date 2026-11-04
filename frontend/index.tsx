import { bootstrapApplication } from '@angular/platform-browser';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { AppComponent } from './src/app.component';
import { provideRouter, withInMemoryScrolling } from '@angular/router';
import { AppRoutes } from './src/app.routes';

bootstrapApplication(AppComponent, {
  providers: [
    provideZonelessChangeDetection(),
    provideHttpClient(),
    provideRouter(AppRoutes, withInMemoryScrolling({ scrollPositionRestoration: 'enabled' }))
  ],
}).catch(err => console.error(err));
