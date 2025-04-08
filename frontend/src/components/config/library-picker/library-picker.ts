import { LitElement, html } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import { GetDir, SetDir } from '@go/library/Library.js';
import { DirectoryPicker } from '@go/frontendbindings/FrontendBindings.js';

@customElement('library-picker')
export class LibraryPicker extends LitElement {

  @property({ type: String })
  currentLibraryDirectoryText: string = "No Library Directory selected";

  override render() {
    return html`
      <div>
        <label>${this.currentLibraryDirectoryText}</label>
        <button @click="${this.onSelectLibraryClick}">Choose Directory...</button>
      </div>
    `;
  }

  constructor() {
    super();
    GetDir().
      then((result) => {
        if (result.length === 0) { return; }
        this.currentLibraryDirectoryText = result;
      }).catch((err) => { console.error(err) })
  }

  onSelectLibraryClick() {
    try {
      DirectoryPicker()
        .then((result: string) => {
          SetDir(result).then(() => {
            this.currentLibraryDirectoryText = result;
          }).catch((err: string) => {
            console.error("There is an error with saving selected directory: " + err);
          });
        })
        .catch((err) => {
          console.error("There is an error with directory picker: " + err);
        });
    }
    catch (err) {
      console.error(err);
    }
  }

}
