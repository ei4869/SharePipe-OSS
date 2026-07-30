import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App, ErrorBoundary } from "./App";
import "./styles/global.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("SharePipe root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </StrictMode>,
);
