import 'htmx.org/dist/htmx.js'
import { DirectoryPicker } from '@go/frontendutil/FrontendUtil';

declare global {
  interface Window { DirectoryPicker: any; }
}

window.DirectoryPicker = DirectoryPicker
