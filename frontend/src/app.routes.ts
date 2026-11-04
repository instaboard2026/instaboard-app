import { Routes } from '@angular/router';

export const AppRoutes: Routes = [
  { path: '', redirectTo: 'dump', pathMatch: 'full' },
  {
    path: 'dump',
    loadComponent: () => import('./app/features/adaptive/pages/dump-page/dump-page.component')
      .then(c => c.DumpPageComponent),
    title: 'Instaboard - Home'
  },
  {
    path: 'folders/:id',
    loadComponent: () => import('./app/features/adaptive/pages/dump-page/dump-page.component')
      .then(c => c.DumpPageComponent),
    title: 'Instaboard - Folder'
  },
  { path: 'account', redirectTo: 'account/profile', pathMatch: 'full' },
  {
    path: 'account/profile',
    loadComponent: () => import('./app/features/adaptive/pages/profile-page/profile-page.component')
      .then(c => c.ProfilePageComponent),
    title: 'Instaboard - Profile'
  },
  { path: '**', redirectTo: 'dump' },
];
