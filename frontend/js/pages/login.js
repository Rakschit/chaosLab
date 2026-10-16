(function () {
  "use strict";

  var form = document.getElementById("login-form");
  var msg = document.getElementById("msg");
  var button = form.querySelector("button[type=submit]");

  function show(text, kind) {
    msg.textContent = text;
    msg.className = "msg" + (kind ? " " + kind : "");
  }

  // Already signed in: skip the form.
  if (AuthAPI.getSession()) {
    window.location.replace(AuthAPI.takeNext() || "dashboard.html");
    return;
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();

    var email = document.getElementById("email").value.trim().toLowerCase();
    var password = document.getElementById("password").value;
    var remember = form.elements["remember"].checked;

    if (email.indexOf("@") < 1) {
      return show("Enter a valid email address.", "err");
    }
    if (!password) {
      return show("Enter your password.", "err");
    }

    button.disabled = true;
    show("Logging in…");

    AuthAPI.post("/login", { email: email, password: password }).then(function (res) {
      if (res.ok && res.data.access_token) {
        AuthAPI.saveSession(res.data, remember);
        show("Logged in. Redirecting…", "good");
        var next = AuthAPI.takeNext() || "dashboard.html";
        setTimeout(function () { window.location.href = next; }, 600);
        return;
      }

      if (res.status === 403 && res.data.error === "email not verified") {
        AuthAPI.rememberEmail(email);
        show("Your email is not verified yet. Redirecting to verification…", "err");
        setTimeout(function () { window.location.href = "verify-email.html"; }, 1500);
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