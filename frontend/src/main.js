import './pico.css';
import { DirectoryPicker } from '../wailsjs/go/main/App';


let libraryDirectoryLabel = document.getElementById("library-directory-label");

window.dirPicker = function () {
  var dir;
  try {
    DirectoryPicker()
    .then((result) => {libraryDirectoryLabel.innerText = result;})
    .catch((err) => {
      console.log("There is an error with directory picker")
      console.error(err);});
  }
  catch (err) {
    console.error(err);
  }
}
