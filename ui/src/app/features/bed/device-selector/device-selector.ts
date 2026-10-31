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

  readonly currentAddress = computed(() => this.store.live().device?.address ?? '');

  // The scan runs every ~15s on the server, so the bed we're actually
  // connected to right now may not have shown up in it yet (just switched,
  // or started against a fixed BED_HOST never discovered). Show it anyway so
  // the dropdown always reflects reality.
  readonly options = computed<DiscoveredDevice[]>(() => {
    const devices = this.store.devices();
    const current = this.currentAddress();
    if (!current || devices.some((d) => d.address === current)) {
      return devices;
    }
    const name = this.store.live().device?.name || current;
    return [{ id: current, name, address: current, class: '' }, ...devices];
  });

  onSelect(address: string): void {
    if (address && address !== this.currentAddress()) {
      this.store.selectDevice(address);
    }
  }
}
