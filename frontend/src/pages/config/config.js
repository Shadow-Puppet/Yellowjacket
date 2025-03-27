import { DirectoryPicker } from '/wailsjs/go/frontendbindings/FrontendBindings.js';
import { GetDir as GetLibraryDir } from '/wailsjs/go/library/Library.js';

let libraryDirectoryLabel = document.getElementById("library-directory-label");

window.updateLibraryDirLabel = function(value) {
  libraryDirectoryLabel.innerText = value;
}

// on load update the library dir label
GetLibraryDir().
  then((result) => {
    if (result.length === 0) { return; }
    window.updateLibraryDirLabel(result);
  }).catch((err) => { console.error(err) })


window.dirPicker = function() {
  try {
    DirectoryPicker()
      .then((result) => { 
        if (result.length === 0 ) { };
        window.updateLibraryDirLabel(result); })
      .catch((err) => {
        console.error("There is an error with directory picker: " + err);
      });
  }
  catch (err) {
    console.error(err);
  }
}
