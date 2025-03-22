import './pico.css';
import { DirectoryPicker, GetLibraryDir } from '../wailsjs/go/main/App';


let libraryDirectoryLabel = document.getElementById("library-directory-label");

GetLibraryDir().
  then((result) => {
    if (result.length === 0) { return; }
    window.updateLibraryDirLabel(result);
  }).catch((err) => { console.error(err) })

window.dirPicker = function() {
  var dir;
  try {
    DirectoryPicker()
      .then((result) => { window.updateLibraryDirLabel(result); })
      .catch((err) => {
        console.log("There is an error with directory picker")
        console.error(err);
      });
  }
  catch (err) {
    console.error(err);
  }
}

window.updateLibraryDirLabel = function(value) {
  libraryDirectoryLabel.innerText = value;
}
