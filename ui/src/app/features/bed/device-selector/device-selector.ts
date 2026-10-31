import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { BedStore } from '../bed-store';
import { DiscoveredDevice } from '../bed-models';

@Component({
  selector: 'app-device-selector',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './device-selector.html',
})
export class DeviceSelector {
  protected readonly store = inject(BedStore);

  // Beds are compared by id: the address is only where the bed is right now
  // and the server follows it when it moves.
  readonly currentId = computed(() => this.store.live().device?.id ?? '');

  // The scan runs every ~15s on the server, so the bed we're actually
  // connected to right now may not have shown up in it yet (just switched).
  // Show it anyway so the dropdown always reflects reality.
  readonly options = computed<DiscoveredDevice[]>(() => {
    const devices = this.store.devices();
    const current = this.currentId();
    if (!current || devices.some((d) => d.id === current)) {
      return devices;
    }
    const device = this.store.live().device;
    const address = device?.address ?? '';
    return [{ id: current, name: device?.name || current, address, class: '' }, ...devices];
  });

  onSelect(id: string): void {
    if (id && id !== this.currentId()) {
      this.store.selectDevice(id);
    }
  }
}
