import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { BedStore } from '../bed-store';
import { DeviceSelector } from '../device-selector/device-selector';

@Component({
  selector: 'app-settings',
  imports: [DeviceSelector],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './settings.html',
})
export class Settings {
  protected readonly store = inject(BedStore);

  readonly statusColor = computed(() => (this.store.controlError() ? '#d9534f' : ''));

  readonly deviceMeta = computed(() => {
    const device = this.store.live().device;
    if (!device?.model) return '';
    return `${device.model} · ${device.configuration ?? ''} · ${device.address ?? ''}`;
  });
}
