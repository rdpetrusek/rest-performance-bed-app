import { ChangeDetectionStrategy, Component } from '@angular/core';

import { LiveCards } from '../live-cards/live-cards';
import { SessionSummary } from '../session-summary/session-summary';

@Component({
  selector: 'app-status',
  imports: [LiveCards, SessionSummary],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <app-live-cards />
    <app-session-summary />
  `,
})
export class Status {}
