import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    loadComponent: () => import('./features/bed/controls/controls').then((m) => m.Controls),
  },
  {
    path: 'status',
    loadComponent: () => import('./features/bed/status/status').then((m) => m.Status),
  },
  {
    path: 'history',
    loadComponent: () => import('./features/bed/history-chart/history-chart').then((m) => m.HistoryChart),
  },
  {
    path: 'settings',
    loadComponent: () => import('./features/bed/settings/settings').then((m) => m.Settings),
  },
  { path: '**', redirectTo: '' },
];
