import {useState} from 'react';
import logo from './assets/images/logo-universal.png';
import './App.css';
import {TQVersion} from "../wailsjs/go/main/App";

function App() {
    const [resultText, setResultText] = useState("Click the button to check for tq 👇");

    function checkVersion() {
        TQVersion().then((v) => setResultText(v || "tq not found on PATH"));
    }

    return (
        <div id="App">
            <img src={logo} id="logo" alt="logo"/>
            <div id="result" className="result">{resultText}</div>
            <div id="input" className="input-box">
                <button className="btn" onClick={checkVersion}>Check tq version</button>
            </div>
        </div>
    )
}

export default App
