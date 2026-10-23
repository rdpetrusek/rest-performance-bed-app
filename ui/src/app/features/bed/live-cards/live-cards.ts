import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { BedStore } from '../bed-store';

@Component({
  selector: 'app-live-cards',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './live-cards.html',
})
export class LiveCards {
  private readonly store = inject(BedStore);

  readonly present = computed(() => {
    const present = this.store.live().body?.present;
    return present === undefined ? '-' : present ? 'Yes' : 'No';
  });

  readonly heartRate = computed(() => {
    const hr = this.store.live().body?.heartrate;
    return hr ? `${hr.toFixed(0)} bpm` : '-';
  });

  readonly respiration = computed(() => {
    const rate = this.store.live().body?.breathing?.rate;
    return rate ? `${rate.toFixed(1)} /min` : '-';
  });

  readonly moving = computed(() => {
    const moving = this.store.live().body?.moving;
    return moving === undefined ? '-' : moving ? 'Moving' : 'Still';
  });

  readonly firmness = computed(() => {
    const firmness = this.store.live().preferences?.firmness;
    return firmness === undefined ? '-' : String(firmness);
  });

  readonly cpuTemp = computed(() => {
    const cpu = this.store.live().temperature?.cpu;
    return cpu === undefined ? '-' : `${cpu.toFixed(1)} C`;
  });

  readonly filling = computed(() => {
    const filling = this.store.live().air?.filling;
    return filling === undefined ? '-' : filling ? 'Filling...' : 'Idle';
  });

  readonly padLabel = computed(() => {
    switch (this.store.padStatus()) {
      case 'connected': {
        const { rows, columns } = this.store.live().surface ?? {};
        return `Connected (${columns}×${rows})`;
      }
      case 'disconnected':
        return 'Not connected';
      case 'bypassed':
        return 'Bypassed';
      default:
        return '-';
    }
  });

  readonly padNote = computed(() => {
    switch (this.store.padStatus()) {
      case 'disconnected':
        return "Pump and valves won't run until the pad reconnects.";
      case 'bypassed':
        return 'z00 configuration: air control works without the pad.';
      default:
        return '';
    }
  });

  readonly padWarning = computed(() => this.store.padStatus() === 'disconnected');

  readonly pressures = computed(() => {
    const pressures = this.store.live().air?.pressures;
    return pressures?.length ? pressures.join(', ') : '-';
  });
}
