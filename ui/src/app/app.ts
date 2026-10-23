import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { BedStore } from './features/bed/bed-store';
import { Controls } from './features/bed/controls/controls';
import { DeviceSelector } from './features/bed/device-selector/device-selector';
import { HistoryChart } from './features/bed/history-chart/history-chart';
import { LiveCards } from './features/bed/live-cards/live-cards';
import { SessionSummary } from './features/bed/session-summary/session-summary';

@Component({
  selector: 'app-root',
  imports: [DeviceSelector, LiveCards, SessionSummary, HistoryChart, Controls],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
})
export class App {
  private readonly store = inject(BedStore);

  readonly deviceName = computed(() => {
    if (!this.store.liveReachable()) return '(unreachable)';
    return this.store.live().device?.name || '(connecting...)';
  });

  readonly deviceMeta = computed(() => {
    const device = this.store.live().device;
    if (!device?.model) return '';
    return `model ${device.model} - ${device.configuration ?? ''} - ${device.address ?? ''}`;
  });
}
