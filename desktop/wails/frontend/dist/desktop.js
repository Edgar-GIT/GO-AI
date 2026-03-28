async function boot() {
  const frame = document.getElementById("app-frame");
  const status = document.getElementById("status-pill");

  const fallbackURL = "http://127.0.0.1:38080/";
  let backendURL = fallbackURL;

  try {
    if (window.go?.main?.App?.BackendURL) {
      backendURL = await window.go.main.App.BackendURL();
      if (!backendURL.endsWith("/")) {
        backendURL += "/";
      }
    }
  } catch (error) {
    console.error("Could not resolve backend URL from Wails binding", error);
  }

  frame.src = backendURL;
  status.textContent = "Local backend running";
}

window.addEventListener("DOMContentLoaded", boot);
