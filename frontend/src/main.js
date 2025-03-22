import './pico.css';
import { DirectoryPicker, GetLibraryDir} from '../wailsjs/go/main/App';
import { Play } from '../wailsjs/go/player/Player';


let libraryDirectoryLabel = document.getElementById("library-directory-label");
let playPauseButtonIcon = document.getElementById("playPauseButtonIcon");

GetLibraryDir().
  then((result) => {
    if (result.length === 0) { return; }
    window.updateLibraryDirLabel(result);
  }).catch((err) => { console.error(err) })

window.onPlayPauseButtonClick = function() {
  try {
    Play().then((result) => {
      console.log(result)
      playPauseButton.setAttribute("src", "/src/assets/images/icons/music/pause-solid.svg")
    }).catch((err) => {
      console.log("There is an error with playing")
      console.error(err);
    });
  }
  catch (err) {
    console.error(err);
  }
}

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
