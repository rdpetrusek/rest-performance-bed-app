import { ChangeDetectionStrategy, Component, ElementRef, signal, viewChild } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';

import { PadAlert } from './features/bed/pad-alert/pad-alert';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, PadAlert],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
  host: {
    '(document:keydown.escape)': 'menuOpen.set(false)',
    '(document:click)': 'closeOnOutsideClick($event)',
  },
})
export class App {
  readonly menuOpen = signal(false);
  private readonly header = viewChild.required<ElementRef<HTMLElement>>('header');

  readonly links = [
    { path: '/', label: 'Controls' },
    { path: '/status', label: 'Status' },
    { path: '/history', label: 'History' },
    { path: '/settings', label: 'Settings' },
  ] as const;

  toggleMenu(): void {
    this.menuOpen.update((open) => !open);
  }

  closeOnOutsideClick(event: MouseEvent): void {
    if (!this.header().nativeElement.contains(event.target as Node)) this.menuOpen.set(false);
  }
}
