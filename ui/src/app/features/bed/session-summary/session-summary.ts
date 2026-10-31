import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { BedStore } from '../bed-store';
import { Stats } from '../bed-models';

function fmtStats(label: string, stats: Stats | null, unit: string, digits = 0): string {
  if (!stats) return `${label}: no data`;
  return `${label}: ${stats.avg.toFixed(digits)} ${unit} avg (${stats.min.toFixed(digits)}-${stats.max.toFixed(digits)})`;
}

@Component({
  selector: 'app-session-summary',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './session-summary.html',
})
export class SessionSummary {
  protected readonly store = inject(BedStore);

  readonly heartRateLine = computed(() => fmtStats('Heart rate', this.store.session()?.heart_rate ?? null, 'bpm'));
  readonly respirationLine = computed(() =>
    fmtStats('Respiration', this.store.session()?.respiration_rate ?? null, '/min', 1),
  );
  readonly movementLine = computed(() => {
    const s = this.store.session();
    if (!s) return '';
    return `Movement events: ${s.movement_events} (${(s.movement_ratio * 100).toFixed(1)}% of samples)`;
  });
  readonly startedAt = computed(() => {
    const start = this.store.session()?.start;
    return start ? new Date(start).toLocaleString() : '';
  });
}
