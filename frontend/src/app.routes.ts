import { Routes } from '@angular/router';

export const AppRoutes: Routes = [
  { path: '', redirectTo: 'home', pathMatch: 'full' },
  {
    path: 'home',
    loadComponent: () => import('./app/features/adaptive/pages/dump-page/dump-page.component')
      .then(c => c.DumpPageComponent),
    title: 'Instaboard - Home'
  },
  // Old bookmarks / packaged links
  { path: 'dump', redirectTo: 'home', pathMatch: 'full' },
  {
    path: 'calendar',
    loadComponent: () => import('./app/features/adaptive/pages/calendar-page/calendar-page.component')
      .then(c => c.CalendarPageComponent),
    title: 'Instaboard - Calendar'
  },
  {
    path: 'folders/:id',
    loadComponent: () => import('./app/features/adaptive/pages/dump-page/dump-page.component')
      .then(c => c.DumpPageComponent),
    title: 'Instaboard - Folder'
  },
  {
    path: 'bin',
    loadComponent: () => import('./app/features/adaptive/pages/dump-page/dump-page.component')
      .then(c => c.DumpPageComponent),
    title: 'Instaboard - Bin'
  },
  { path: 'account', redirectTo: 'account/profile', pathMatch: 'full' },
  {
    path: 'account/profile',
    loadComponent: () => import('./app/features/adaptive/pages/profile-page/profile-page.component')
      .then(c => c.ProfilePageComponent),
    title: 'Instaboard - Profile'
  },
  { path: '**', redirectTo: 'home' },
];
