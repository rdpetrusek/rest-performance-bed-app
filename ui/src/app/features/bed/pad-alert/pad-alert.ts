import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { BedStore } from '../bed-store';

/** Shown only while the pad is required but not reporting - the one case where air control stops. */
@Component({
  selector: 'app-pad-alert',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (show()) {
      <div class="alert" role="alert">
        <strong>Sensor pad not responding</strong>
        <button type="button" (click)="store.togglePadBypass()">Bypass sensor</button>
      </div>
    }
  `,
})
export class PadAlert {
  protected readonly store = inject(BedStore);

  readonly show = computed(() => this.store.padStatus() === 'disconnected' && this.store.padConfig() !== null);
}
