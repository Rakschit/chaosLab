(function () {
  "use strict";

  var form = document.getElementById("reset-form");
  var msg = document.getElementById("msg");
  var button = document.getElementById("save");
  var dot = document.getElementById("status-dot");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  var token = new URLSearchParams(window.location.search).get("token");

  // Keep the one-time token out of the address bar, history and Referer headers.
  try { window.history.replaceState(null, "", window.location.pathname); } catch (e) { /* ignore */ }

  if (!token) {
    form.style.display = "none";
    dot.className = "dot bad";
    show("This page needs the link from your reset email.", "err");
    return;
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    var password = document.getElementById("password").value;
    var confirm = document.getElementById("confirm").value;

    // The API limits passwords by bytes (bcrypt reads at most 72).
    var bytes = new TextEncoder().encode(password).length;
    if (bytes < 12) return show("Password must be at least 12 characters.", "err");
    if (bytes > 72) return show("Password is too long (72 bytes at most).", "err");
    if (password !== confirm) return show("The two passwords do not match.", "err");

    button.disabled = true;
    show("Saving…");

    AuthAPI.post("/reset-password", { token: token, password: password }).then(function (res) {
      if (res.ok) {
        AuthAPI.clearSession(); // every old session was revoked server-side
        form.style.display = "none";
        document.getElementById("links").style.display = "none";
        show((res.data.message || "Password updated.") + " Redirecting to log in…", "good");
        setTimeout(function () { window.location.href = "login.html"; }, 1800);
        return;
      }
      show(res.data.error || "Something went wrong. Please try again.", "err");
      button.disabled = false;
    }).catch(function () {
      show("Could not reach the server. Please try again.", "err");
      button.disabled = false;
    });
  });
})();
