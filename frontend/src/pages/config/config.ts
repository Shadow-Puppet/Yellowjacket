import 'htmx.org/dist/htmx.js'
import { DirectoryPicker } from '@go/frontendutil/FrontendUtil';
import { Scan } from '@go/library/Library';

declare global {
  interface Window {
    DirectoryPicker: typeof DirectoryPicker;
    Scan: typeof Scan;
    selectLibraryDirectory: (pElement: HTMLInputElement) => void;
    scanLibrary: (button: HTMLButtonElement) => void;
  }
}

window.DirectoryPicker = DirectoryPicker;
window.Scan = Scan;

window.selectLibraryDirectory = (pElement: HTMLInputElement) => {
  window.DirectoryPicker()
    .then((result) => {
      if (result.length !== 0) {
        pElement.value = result;
      }
    })
    .catch((err: unknown) => {
      console.error('error with directory picker: ' + err);
    });
};

window.scanLibrary = (button: HTMLButtonElement) => {
  button.disabled = true;
  button.textContent = 'Scanning...';
  window.Scan()
    .then(() => {
      button.textContent = 'Scan Library';
      button.disabled = false;
    })
    .catch((err: unknown) => {
      console.error('error scanning library: ' + err);
      button.textContent = 'Scan Library';
      button.disabled = false;
    });
};
