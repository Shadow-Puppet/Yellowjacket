import './pico.css';
import {Greet} from '../wailsjs/go/main/App';

document.querySelector('#app').innerHTML = `
      <div class="result" id="result">Please enter your name below 👇</div>
      <div class="input-box" id="input">
        <input class="input" id="name" type="text" autocomplete="off" />
        <button class="btn" onclick="greet()">Greet</button>
        <input type="file" id="directory-picker" name="fileList" webkitdirectory directory multiple/>
        <ul id="listing"></ul>
        </div>
    </div>
`;
  

let nameElement = document.getElementById("name");
nameElement.focus();
let resultElement = document.getElementById("result");

document.getElementById("directory-picker").addEventListener(
    "change",
    (event) => {
      let output = document.getElementById("listing");
      for (const file of event.target.files) {
        let item = document.createElement("li");
        item.textContent = file.webkitRelativePath;
        output.appendChild(item);
      }
    },
    false,
  );
  

// Setup the greet function
window.greet = function () {
    // Get name
    let name = nameElement.value;

    // Check if the input is empty
    if (name === "") return;

    // Call App.Greet(name)
    try {
        Greet(name)
            .then((result) => {
                // Update result with data back from App.Greet()
                resultElement.innerText = result;
            })
            .catch((err) => {
                console.error(err);
            });
    } catch (err) {
        console.error(err);
    }
};
