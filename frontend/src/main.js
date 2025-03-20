import './pico.css';
import { DirectoryPicker } from '../wailsjs/go/main/App';


let libraryDirectoryElement = document.getElementById("library-directory");

window.dirPicker = function () {
  var dir;
  try {
    DirectoryPicker()
    .then((result) => {libraryDirectoryElement.innerText = result;})
    .catch((err) => {
      console.log("There is an error with directory picker")
      console.error(err);});
  }
  catch (err) {
    console.error(err);
  }
}
