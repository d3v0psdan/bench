import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";
import { installDesktopShell } from "./lib/desktop-shell";

installDesktopShell();

// index.html always ships the #root element.
ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
